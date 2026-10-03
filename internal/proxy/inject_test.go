package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ── 注入频率调度（inject.go）单元测试 ──

// N=20：第 1、21、41 次注入，其余不注入。
func TestInjectSchedFrequency(t *testing.T) {
	var s injectSched
	now := time.Unix(0, 0)
	var got []int
	for i := 1; i <= 41; i++ {
		if s.isDue("s", 20, now) {
			got = append(got, i)
		}
	}
	want := []int{1, 21, 41}
	if len(got) != len(want) {
		t.Fatalf("注入点 = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("注入点 = %v, want %v", got, want)
		}
	}
}

// 按会话隔离：两会话交错，各自第 1 次都注入。
func TestInjectSchedPerSession(t *testing.T) {
	var s injectSched
	now := time.Unix(0, 0)
	got := map[string][]int{}
	for i := 1; i <= 4; i++ {
		for _, sess := range []string{"A", "B"} {
			if s.isDue(sess, 2, now) {
				got[sess] = append(got[sess], i)
			}
		}
	}
	// 每会话第 1、3 轮注入（每 2 次 1 次，首次注入）
	if len(got["A"]) != 2 || got["A"][0] != 1 || got["A"][1] != 3 {
		t.Errorf("会话 A 注入点 = %v, want [1 3]", got["A"])
	}
	if len(got["B"]) != 2 || got["B"][0] != 1 || got["B"][1] != 3 {
		t.Errorf("会话 B 注入点 = %v, want [1 3]", got["B"])
	}
}

// N<=1（含 0 与负数）= 每次都注入（兼容旧行为）。
func TestInjectSchedEveryOne(t *testing.T) {
	var s injectSched
	now := time.Unix(0, 0)
	for _, every := range []int{0, 1, -3} {
		for i := 0; i < 5; i++ {
			if !s.isDue("s", every, now) {
				t.Fatalf("every=%d 第 %d 次应注入", every, i+1)
			}
		}
	}
}

// 空会话 ID 走同一个桶，也不 panic。
func TestInjectSchedEmptySession(t *testing.T) {
	var s injectSched
	now := time.Unix(0, 0)
	if !s.isDue("", 3, now) {
		t.Fatal("首次应注入")
	}
	if s.isDue("", 3, now) {
		t.Fatal("第 2 次不应注入")
	}
}

// 计数器有界：超量会话触发清理，行为不被破坏。
func TestInjectSchedPrune(t *testing.T) {
	var s injectSched
	old := time.Unix(0, 0)
	for i := 0; i < 5000; i++ {
		s.isDue(string(rune('a'+i%26))+string(rune('0'+i/26%10))+string(rune('A'+i/260%26)), 2, old)
	}
	s.mu.Lock()
	n := len(s.m)
	s.mu.Unlock()
	if n > 5000 {
		t.Fatalf("计数器无界增长: %d", n)
	}
	// 活跃会话仍按节奏走
	if !s.isDue("live", 2, time.Unix(1000, 0)) {
		t.Fatal("新会话首次应注入")
	}
}

// ── 端到端：经 handler 的注入频率 ──

func doRequestSession(t *testing.T, e *Engine, body, session string) (RequestRecord, []byte) {
	t.Helper()
	var captured RequestRecord
	e.opts.Events = &captureSink{rec: &captured}

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Session-Id", session)
	w := httptest.NewRecorder()
	e.handle(w, req)
	return captured, w.Body.Bytes()
}

const freqReqBody = `{"model":"m","stream":false,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`

// 每 3 次注入 1 次：7 个请求里恰好第 1、4、7 次注入。
func TestPipelineInjectFrequency(t *testing.T) {
	up := &scriptedUpstream{responses: []stubResponse{{status: 200, body: `{"output":[]}`}}}
	cfg := &testConfig{
		instruction: "INSTRUCTION-XYZ",
		injectEvery: 3,
		tamper:      &stubTamper{},
		retry:       RetryOptions{Enabled: false},
	}
	e, _ := newTestEngine(t, up, cfg)

	var injectedAt []int
	for i := 1; i <= 7; i++ {
		rec, _ := doRequestSession(t, e, freqReqBody, "sess-1")
		if rec.Injected {
			injectedAt = append(injectedAt, i)
		}
	}
	want := []int{1, 4, 7}
	if len(injectedAt) != len(want) {
		t.Fatalf("注入点 = %v, want %v", injectedAt, want)
	}
	for i := range want {
		if injectedAt[i] != want[i] {
			t.Fatalf("注入点 = %v, want %v", injectedAt, want)
		}
	}
	// 上游确实只在注入点收到指令
	n := 0
	for _, b := range up.gotBodies {
		if strings.Contains(string(b), "INSTRUCTION-XYZ") {
			n++
		}
	}
	if n != 3 {
		t.Errorf("上游收到指令 %d 次, want 3", n)
	}
}

// 按会话计数：两会话交错请求时各自首次注入（全局计数则不会如此）。
func TestPipelineInjectFrequencyPerSession(t *testing.T) {
	up := &scriptedUpstream{responses: []stubResponse{{status: 200, body: `{"output":[]}`}}}
	cfg := &testConfig{
		instruction: "INSTRUCTION-XYZ",
		injectEvery: 2,
		tamper:      &stubTamper{},
		retry:       RetryOptions{Enabled: false},
	}
	e, _ := newTestEngine(t, up, cfg)

	// A、B 交错各 2 次：A1 注入、B1 注入、A2 不注入、B2 不注入
	seq := []struct {
		sess string
		want bool
	}{
		{"A", true}, {"B", true}, {"A", false}, {"B", false},
	}
	for i, s := range seq {
		rec, _ := doRequestSession(t, e, freqReqBody, s.sess)
		if rec.Injected != s.want {
			t.Errorf("第 %d 次 %s 注入=%v, want %v", i+1, s.sess, rec.Injected, s.want)
		}
	}
}

// N=1（默认）：每次都注入，旧行为不变。
func TestPipelineInjectEveryDefault(t *testing.T) {
	up := &scriptedUpstream{responses: []stubResponse{{status: 200, body: `{"output":[]}`}}}
	cfg := &testConfig{
		instruction: "INSTRUCTION-XYZ",
		tamper:      &stubTamper{},
		retry:       RetryOptions{Enabled: false},
	}
	e, _ := newTestEngine(t, up, cfg)

	for i := 0; i < 3; i++ {
		rec, _ := doRequestSession(t, e, freqReqBody, "sess")
		if !rec.Injected {
			t.Fatalf("第 %d 次应注入（injectEvery 默认 1）", i+1)
		}
	}
}
