package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ── 注入频率调度（inject.go）单元测试 ──
//
// 模拟约定：每次 shouldAttempt 为 true 且"注入成功"时调 markInjected；
// 失败则不调 —— 语义见 inject.go 头注释。

// N=3 会话采样：第 1、4、7 个会话命中；命中会话全程跟随，未命中全程不注。
func TestInjectSchedSessionSampling(t *testing.T) {
	var s injectSched
	now := time.Unix(0, 0)
	for i := 1; i <= 7; i++ {
		sess := "s" + string(rune('0'+i))
		want := i%3 == 1
		first := s.shouldAttempt(sess, 3, now)
		if first != want {
			t.Fatalf("会话 %d 首请求命中=%v, want %v", i, first, want)
		}
		if first {
			s.markInjected(sess, 3, now)
			for j := 0; j < 3; j++ {
				if !s.shouldAttempt(sess, 3, now) {
					t.Fatalf("会话 %d 跟随态第 %d 个续轮应继续注入", i, j+1)
				}
			}
		} else {
			for j := 0; j < 3; j++ {
				if s.shouldAttempt(sess, 3, now) {
					t.Fatalf("会话 %d 未命中，续轮 %d 不应注入", i, j+1)
				}
			}
		}
	}
}

// 注入失败不占名额：命中会话连续失败会一直重试，成功后进入跟随态。
func TestInjectSchedFailureDoesNotConsume(t *testing.T) {
	var s injectSched
	now := time.Unix(0, 0)

	// 第 1~4 次都到注入点但注入失败（不调 markInjected）→ 每次都重试
	for i := 1; i <= 4; i++ {
		if !s.shouldAttempt("s", 20, now) {
			t.Fatalf("第 %d 次失败后应立即重试注入", i)
		}
		// 注入失败：不调 markInjected
	}
	// 第 5 次成功 → 跟随态：后续请求持续注入（续轮不再裸奔）
	if !s.shouldAttempt("s", 20, now) {
		t.Fatal("第 5 次应仍是注入点")
	}
	s.markInjected("s", 20, now)
	for i := 6; i <= 30; i++ {
		if !s.shouldAttempt("s", 20, now) {
			t.Fatalf("第 %d 次应注入（跟随态全程携带）", i)
		}
	}
}

// N<=1（含 0 与负数）= 每次都注入（兼容旧行为）。
func TestInjectSchedEveryOne(t *testing.T) {
	var s injectSched
	now := time.Unix(0, 0)
	for _, every := range []int{0, 1, -3} {
		for i := 0; i < 5; i++ {
			if !s.shouldAttempt("s", every, now) {
				t.Fatalf("every=%d 第 %d 次应注入", every, i+1)
			}
		}
	}
}

// 空会话 ID 走同一个桶（一个会话），也不 panic。
func TestInjectSchedEmptySession(t *testing.T) {
	var s injectSched
	now := time.Unix(0, 0)
	if !s.shouldAttempt("", 3, now) {
		t.Fatal("首个会话应命中")
	}
	s.markInjected("", 3, now)
	if !s.shouldAttempt("", 3, now) {
		t.Fatal("跟随态应继续注入")
	}
}

// 计数器有界：超量会话触发清理，行为不被破坏。
func TestInjectSchedPrune(t *testing.T) {
	var s injectSched
	old := time.Unix(0, 0)
	for i := 0; i < 5000; i++ {
		s.shouldAttempt(string(rune('a'+i%26))+string(rune('0'+i/26%10))+string(rune('A'+i/260%26)), 2, old)
	}
	s.mu.Lock()
	n := len(s.m)
	s.mu.Unlock()
	if n > 5000 {
		t.Fatalf("计数器无界增长: %d", n)
	}
	// 活跃新会话仍按采样走（every=2：奇数序会话命中）
	if !s.shouldAttempt("live", 2, time.Unix(1000, 0)) {
		t.Fatal("新会话命中位应注入")
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

// 命中会话全程跟随：every=3 下同一会话 7 个请求全部注入（续轮不再裸奔）。
func TestPipelineInjectStickyWithinSession(t *testing.T) {
	up := &scriptedUpstream{responses: []stubResponse{{status: 200, body: `{"output":[]}`}}}
	cfg := &testConfig{
		instruction: "INSTRUCTION-XYZ",
		injectEvery: 3,
		tamper:      &stubTamper{},
		retry:       RetryOptions{Enabled: false},
	}
	e, _ := newTestEngine(t, up, cfg)

	for i := 1; i <= 7; i++ {
		rec, _ := doRequestSession(t, e, freqReqBody, "sess-1")
		if !rec.Injected {
			t.Fatalf("第 %d 次应注入（命中会话跟随态全程携带）", i)
		}
	}
	// 上游每次都收到指令
	n := 0
	for _, b := range up.gotBodies {
		if strings.Contains(string(b), "INSTRUCTION-XYZ") {
			n++
		}
	}
	if n != 7 {
		t.Errorf("上游收到指令 %d 次, want 7", n)
	}
}

// 会话采样：every=2 时第 1 个会话命中（全程注入），第 2 个会话不命中（全程不注）。
func TestPipelineInjectSamplingPerSession(t *testing.T) {
	up := &scriptedUpstream{responses: []stubResponse{{status: 200, body: `{"output":[]}`}}}
	cfg := &testConfig{
		instruction: "INSTRUCTION-XYZ",
		injectEvery: 2,
		tamper:      &stubTamper{},
		retry:       RetryOptions{Enabled: false},
	}
	e, _ := newTestEngine(t, up, cfg)

	// A、B 交错各 2 次：A 命中（A1/A2 都注入），B 不命中（B1/B2 都不注）
	seq := []struct {
		sess string
		want bool
	}{
		{"A", true}, {"B", false}, {"A", true}, {"B", false},
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
