package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// scriptedUpstream 按脚本返回响应，用于驱动管线各分支。
type scriptedUpstream struct {
	responses []stubResponse
	mu        atomic.Int64 // 当前已服务的请求数
	gotBodies [][]byte
}

type stubResponse struct {
	status int
	ct     string
	body   string
}

func (s *scriptedUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	idx := int(s.mu.Add(1)) - 1
	s.gotBodies = append(s.gotBodies, body)

	var resp stubResponse
	if idx < len(s.responses) {
		resp = s.responses[idx]
	} else if len(s.responses) > 0 {
		resp = s.responses[len(s.responses)-1] // 用最后一条兜底
	} else {
		resp = stubResponse{status: 200, ct: "application/json", body: `{"output":[]}`}
	}
	if resp.ct == "" {
		resp.ct = "application/json"
	}
	w.Header().Set("Content-Type", resp.ct)
	w.WriteHeader(resp.status)
	_, _ = io.WriteString(w, resp.body)
}

// newTestEngine 起一个指向假上游的引擎。
func newTestEngine(t *testing.T, up *scriptedUpstream, cfg ConfigProvider) (*Engine, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)

	e, err := New(Options{
		Listen:            "127.0.0.1:0",
		Upstream:          srv.URL + "/v1",
		Config:            cfg,
		Retry:             RetryOptions{Enabled: false},
		StreamWindowBytes: 64,
		StreamWindowMs:    50,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return e, srv
}

// testConfig 是固定的配置提供者。
type testConfig struct {
	instruction string
	injectEvery int
	fallbackUA  string
	tamper      *stubTamper
	// retry 必须显式设置：零值表示"不重试"，是合法配置
	retry RetryOptions
}

func (c *testConfig) PromptInstruction() string { return c.instruction }
func (c *testConfig) Rewriter() Rewriter        { return nil }
func (c *testConfig) Tamper() TamperMatcher     { return c.tamper }
func (c *testConfig) Retry() RetryOptions       { return c.retry }
func (c *testConfig) FallbackUserAgent() string { return c.fallbackUA }
func (c *testConfig) InjectEvery() int {
	if c.injectEvery <= 0 {
		return 1
	}
	return c.injectEvery
}

type stubTamper struct{ marker string }

func (s *stubTamper) IsRefusal(text string) bool { return strings.Contains(text, "无法协助") }
func (s *stubTamper) Attach(text string) string  { return s.marker + text }
func (s *stubTamper) Marker() string {
	if s.marker == "" {
		return DefaultTestMarker
	}
	return s.marker
}

const DefaultTestMarker = "[MARKER]\n"

// 正常请求：注入生效 + 响应透传。
func TestPipelineHealthy(t *testing.T) {
	up := &scriptedUpstream{responses: []stubResponse{{
		status: 200,
		body:   `{"output":[{"content":[{"text":"正常输出"}]}]}`,
	}}}
	cfg := &testConfig{instruction: "INSTRUCTION-XYZ", tamper: &stubTamper{}, retry: RetryOptions{Enabled: false}}
	e, _ := newTestEngine(t, up, cfg)

	reqBody := `{"model":"m","stream":false,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`
	// 本用例断言的是发往上游的请求体，响应体不参与断言
	rec, _ := doRequest(t, e, reqBody)

	if rec.Class != ClassHealthy {
		t.Errorf("class = %s, want Healthy (note=%s)", rec.Class, rec.Note)
	}
	if !rec.Injected {
		t.Error("应记录为已注入")
	}
	// 上游收到的 body 必须含注入的指令
	if len(up.gotBodies) == 0 {
		t.Fatal("上游未收到请求")
	}
	if !strings.Contains(string(up.gotBodies[0]), "INSTRUCTION-XYZ") {
		t.Error("上游收到的请求未包含注入指令")
	}
	// 且必须在 input[0] 位置（与实测验证过的注入点一致）
	got := string(up.gotBodies[0])
	if !strings.Contains(got, `"input":[{"content":[{"text":"INSTRUCTION-XYZ","type":"input_text"}],"role":"system","type":"message"},`) {
		// 字段顺序可能不同，退化为检查 system 出现在第一个 input 条目
		idxInput := strings.Index(got, `"input":[`)
		idxInstr := strings.Index(got, "INSTRUCTION-XYZ")
		idxSecond := strings.Index(got[idxInput:], `"type":"message"`)
		if idxInstr < 0 || idxSecond < 0 || idxInstr > idxInput+idxSecond {
			t.Errorf("注入位置不在 input[0]：%s", truncForLog(got, 300))
		}
	}
}

// 不通入：passthrough 模式不改任何内容。
func TestPipelinePassthrough(t *testing.T) {
	up := &scriptedUpstream{responses: []stubResponse{{status: 200, body: `{"output":[]}`}}}
	cfg := &testConfig{instruction: "SHOULD-NOT-APPEAR", tamper: &stubTamper{}, retry: RetryOptions{Enabled: false}}

	srv := httptest.NewServer(up)
	defer srv.Close()
	e, err := New(Options{
		Listen: "127.0.0.1:0", Upstream: srv.URL + "/v1",
		Config: cfg, Passthrough: true, Retry: RetryOptions{Enabled: false},
	})
	if err != nil {
		t.Fatal(err)
	}

	reqBody := `{"model":"m","input":[]}`
	_, _ = doRequest(t, e, reqBody)

	if strings.Contains(string(up.gotBodies[0]), "SHOULD-NOT-APPEAR") {
		t.Error("passthrough 模式不应注入")
	}
}

// ★ INV-8：补救时必须保留模型原文。
func TestRemedyPreservesOriginalText(t *testing.T) {
	const original = "抱歉，我无法协助这个请求。"
	// 第一次返回拒绝，第二次（clean session 重发）仍返回拒绝
	up := &scriptedUpstream{responses: []stubResponse{
		{status: 200, body: `{"output":[{"content":[{"text":"` + original + `"}]}]}`},
		{status: 200, body: `{"output":[{"content":[{"text":"` + original + `"}]}]}`},
	}}
	cfg := &testConfig{tamper: &stubTamper{marker: "[MARKER]\n"}, retry: RetryOptions{Enabled: false}}
	e, _ := newTestEngine(t, up, cfg)

	reqBody := `{"model":"m","stream":false,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"test"}]}]}`
	rec, respBody := doRequest(t, e, reqBody)

	if rec.Class == ClassHealthy {
		t.Fatal("应被判为拒绝或未补救")
	}

	// 关键断言：响应正文里必须同时有标记和模型原话
	out := respBody
	if !strings.Contains(string(out), "[MARKER]") {
		t.Errorf("响应缺少标记：%s", truncForLog(string(out), 300))
	}
	if !strings.Contains(string(out), original) {
		t.Errorf("★ INV-8 违反：模型原文被删除。响应=%s", truncForLog(string(out), 400))
	}
}

// 补救成功：第二次返回正常内容，应采纳并记为 retry。
func TestRemedyRetrySucceeds(t *testing.T) {
	up := &scriptedUpstream{responses: []stubResponse{
		{status: 200, body: `{"output":[{"content":[{"text":"我无法协助"}]}]}`},
		{status: 200, body: `{"output":[{"content":[{"text":"这次可以了"}]}]}`},
	}}
	cfg := &testConfig{tamper: &stubTamper{}, retry: RetryOptions{Enabled: false}}
	e, _ := newTestEngine(t, up, cfg)

	rec, respBody := doRequest(t, e, `{"model":"m","stream":false,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"t"}]}]}`)

	if rec.Action != ActionRetry {
		t.Errorf("action = %s, want retry (note=%s)", rec.Action, rec.Note)
	}
	if rec.Class != ClassHealthy {
		t.Errorf("class = %s, want Healthy", rec.Class)
	}
	if !strings.Contains(string(respBody), "这次可以了") {
		t.Error("未采纳重发的结果")
	}
}

// ★ 上游失败绝不触发补救（不额外发请求）。
func TestUpstreamFailedSkipsRemedy(t *testing.T) {
	up := &scriptedUpstream{responses: []stubResponse{
		{status: 502, body: `{"error":{"message":"boom"}}`},
	}}
	cfg := &testConfig{tamper: &stubTamper{}, retry: RetryOptions{Enabled: false}}
	e, _ := newTestEngine(t, up, cfg)

	// 本用例断言判定与上游调用次数，响应体不参与断言
	rec, _ := doRequest(t, e, `{"model":"m","stream":false,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"t"}]}]}`)

	if rec.Class != ClassUpstreamFailed {
		t.Errorf("class = %s, want UpstreamFailed", rec.Class)
	}
	if rec.Action != ActionNone {
		t.Errorf("action = %s, want none —— 上游失败不应补救", rec.Action)
	}
	if got := up.mu.Load(); got != 1 {
		t.Errorf("上游收到 %d 次请求，应为 1 次（不重试）", got)
	}
}

// 请求体无法解析 → 原样透传（INV-9），不猜。
func TestMalformedRequestPassesThrough(t *testing.T) {
	up := &scriptedUpstream{responses: []stubResponse{{status: 200, body: `{"output":[]}`}}}
	cfg := &testConfig{instruction: "X", tamper: &stubTamper{}, retry: RetryOptions{Enabled: false}}
	e, _ := newTestEngine(t, up, cfg)

	const broken = `{"model":"m","input":[BROKEN`
	_, _ = doRequest(t, e, broken)

	if string(up.gotBodies[0]) != broken {
		t.Errorf("无法解析的请求应原样透传\n got: %s\nwant: %s", up.gotBodies[0], broken)
	}
}

// 干跑模式：不发上游。
func TestDryRunNoUpstreamCall(t *testing.T) {
	up := &scriptedUpstream{}
	cfg := &testConfig{instruction: "INSTR", tamper: &stubTamper{}, retry: RetryOptions{Enabled: false}}

	srv := httptest.NewServer(up)
	defer srv.Close()
	e, err := New(Options{
		Listen: "127.0.0.1:0", Upstream: srv.URL + "/v1",
		Config: cfg, DryRun: true, Retry: RetryOptions{Enabled: false},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, _ = doRequest(t, e, `{"model":"m","stream":false,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"t"}]}]}`)

	if n := up.mu.Load(); n != 0 {
		t.Errorf("干跑不应发上游，实际发了 %d 次", n)
	}
}

// ── 测试辅助 ──

// doRequest 直接把请求交给引擎的 handler（不经真实端口），并捕获响应。
//
// 【为何返回两个值】早期实现把响应体存在包级变量 lastResponse 里。
// 那是共享可变状态：多个测试写同一个变量，一旦执行顺序交错就会互相覆盖，
// 表现为「单独跑通过、全量跑失败」——实测在 ./internal/... 下确曾失败过。
// 现在改为返回值传递，测试之间零共享状态。
func doRequest(t *testing.T, e *Engine, body string) (RequestRecord, []byte) {
	t.Helper()
	var captured RequestRecord
	e.opts.Events = &captureSink{rec: &captured}

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		e.handle(w, req)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("handler 超时")
	}
	return captured, w.Body.Bytes()
}

// captureSink 只捕获请求记录。
type captureSink struct {
	rec *RequestRecord
}

func (c *captureSink) Log(level, msg string) {}
func (c *captureSink) Request(r RequestRecord) {
	*c.rec = r
}

func truncForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

var _ = fmt.Sprintf
var _ = context.Background
