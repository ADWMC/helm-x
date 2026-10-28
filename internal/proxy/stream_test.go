package proxy

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// sseRefusal 是一段"模型在拒绝"的 SSE。
const sseRefusal = "event: response.output_text.delta\n" +
	"data: {\"type\":\"response.output_text.delta\",\"delta\":\"抱歉，我无法协助这个请求。\"}\n\n"

// sseHealthy 是一段正常回答的 SSE。
const sseHealthy = "event: response.output_text.delta\n" +
	"data: {\"type\":\"response.output_text.delta\",\"delta\":\"这里是完整的技术方案。\"}\n\n"

// ★ P0-1：流式窗口内命中拒答必须真的走补救（旧实现只打了个 note）。
//
// 期望：上游被请求两次（原始 + clean session 重发），客户端拿到的是**重发
// 成功的那份内容** —— 用户全程看不到拒绝。
func TestStreamRefusalTriggersRemedy(t *testing.T) {
	up := &scriptedUpstream{responses: []stubResponse{
		{status: 200, ct: "text/event-stream", body: sseRefusal},
		{status: 200, ct: "text/event-stream", body: sseHealthy},
	}}
	cfg := &testConfig{instruction: "INSTRUCTION", tamper: &stubTamper{}, retry: RetryOptions{Enabled: false}}
	e, srv := newTestEngine(t, up, cfg)
	_ = srv

	reqBody := `{"model":"m","stream":true,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`
	body, rec := doStreamRequest(t, e, reqBody)

	if rec.Upstream != 2 {
		t.Errorf("上游请求数 = %d, want 2（原始 + 补救重发）", rec.Upstream)
	}
	if rec.Action != ActionRetry {
		t.Errorf("action = %s, want %s (note=%s)", rec.Action, ActionRetry, rec.Note)
	}
	if rec.Class != ClassHealthy {
		t.Errorf("class = %s, want Healthy", rec.Class)
	}
	if strings.Contains(body, "无法协助") {
		t.Errorf("客户端不应看到被拒内容，实得:\n%s", body)
	}
	if !strings.Contains(body, "完整的技术方案") {
		t.Errorf("客户端应拿到重发成功的内容，实得:\n%s", body)
	}
}

// 重发仍被拒 → 进第 2 级：marker 前置，原文一个字节不删（INV-8）。
func TestStreamRefusalStillRefusedAttachesMarker(t *testing.T) {
	up := &scriptedUpstream{responses: []stubResponse{
		{status: 200, ct: "text/event-stream", body: sseRefusal},
		{status: 200, ct: "text/event-stream", body: sseRefusal},
	}}
	cfg := &testConfig{instruction: "INSTRUCTION", tamper: &stubTamper{}, retry: RetryOptions{Enabled: false}}
	e, _ := newTestEngine(t, up, cfg)

	reqBody := `{"model":"m","stream":true,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`
	body, rec := doStreamRequest(t, e, reqBody)

	if rec.Action != ActionAttachMarker {
		t.Fatalf("action = %s, want %s (note=%s)", rec.Action, ActionAttachMarker, rec.Note)
	}
	if !strings.Contains(body, "[MARKER]") {
		t.Errorf("应在正文前插入标记，实得:\n%s", body)
	}
	if !strings.Contains(body, "无法协助") {
		t.Errorf("★ INV-8 违反：原文被删除，实得:\n%s", body)
	}
	if !strings.HasPrefix(body, "event: response.output_text.delta") {
		t.Errorf("标记应是合法的 SSE 事件，实得前缀:\n%.80s", body)
	}
}

// 窗口外的拒答（已开始转发）不补救，但必须记录为 Refused。
func TestStreamRefusalAfterWindowIsRecorded(t *testing.T) {
	long := strings.Repeat("x", 4096)
	up := &scriptedUpstream{responses: []stubResponse{
		{status: 200, ct: "text/event-stream", body: "event: response.output_text.delta\ndata: {\"delta\":\"" + long + "\"}\n\n" + sseRefusal},
	}}
	cfg := &testConfig{instruction: "INSTRUCTION", tamper: &stubTamper{}, retry: RetryOptions{Enabled: false}}
	e, _ := newTestEngine(t, up, cfg)

	reqBody := `{"model":"m","stream":true,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`
	_, rec := doStreamRequest(t, e, reqBody)

	if rec.Class != ClassRefused {
		t.Errorf("class = %s, want Refused（已转发但仍是拒绝）", rec.Class)
	}
	if rec.Action != ActionNone {
		t.Errorf("action = %s, want none（内容已转发，不能再补救）", rec.Action)
	}
}

// 流式也要重试：首个 500 之后成功，客户端不应看到 502。
func TestStreamRetriesOnUpstream500(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n == 1 {
			w.WriteHeader(500)
			_, _ = io.WriteString(w, `{"error":"boom"}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, sseHealthy)
	}))
	t.Cleanup(srv.Close)

	e, err := New(Options{
		Listen:            "127.0.0.1:0",
		Upstream:          srv.URL,
		Config:            &testConfig{tamper: &stubTamper{}, retry: RetryOptions{Enabled: true, MaxRetries: 2, DelaySeconds: 0}},
		StreamWindowBytes: 64,
		StreamWindowMs:    50,
	})
	if err != nil {
		t.Fatal(err)
	}
	reqBody := `{"model":"m","stream":true,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`
	body, rec := doStreamRequest(t, e, reqBody)

	if got := atomic.LoadInt32(&hits); got < 2 {
		t.Errorf("上游请求数 = %d, want >= 2（应重试）", got)
	}
	if rec.Class != ClassHealthy {
		t.Errorf("class = %s, want Healthy (note=%s)", rec.Class, rec.Note)
	}
	if !strings.Contains(body, "完整的技术方案") {
		t.Errorf("应拿到重试后的内容，实得:\n%s", body)
	}
}

// 上游中途断开（Content-Length 大于实际字节）必须记 UpstreamFailed，
// 而不是旧实现那样一律 Healthy。
func TestStreamTruncationIsUpstreamFailed(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, buf, herr := hj.Hijack()
		if herr != nil {
			return
		}
		// 声称 4096 字节，只给 40 字节就断开
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nContent-Length: 4096\r\n\r\n")
		_, _ = buf.WriteString("event: response.output_text.delta\ndata: {\"delta\":\"x\"}\n\n")
		_ = buf.Flush()
		_ = conn.Close()
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	e, err := New(Options{
		Listen:            "127.0.0.1:0",
		Upstream:          "http://" + ln.Addr().String(),
		Config:            &testConfig{tamper: &stubTamper{}},
		Retry:             RetryOptions{Enabled: false},
		StreamWindowBytes: 64,
		StreamWindowMs:    50,
	})
	if err != nil {
		t.Fatal(err)
	}
	reqBody := `{"model":"m","stream":true,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`
	_, rec := doStreamRequest(t, e, reqBody)

	if rec.Class != ClassUpstreamFailed {
		t.Errorf("class = %s, want UpstreamFailed (note=%s)", rec.Class, rec.Note)
	}
}

// doStreamRequest 起引擎并发一次流式请求，返回响应体与请求记录。
func doStreamRequest(t *testing.T, e *Engine, reqBody string) (string, RequestRecord) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	recs := make(chan RequestRecord, 4)
	sink := &recSink{ch: recs}
	e.opts.Events = sink
	t.Cleanup(func() {
		for _, l := range sink.dump() {
			t.Logf("engine log | %s", l)
		}
	})

	go func() { _ = e.Start(ctx) }()
	waitRunning(t, e, 3*time.Second)

	resp, err := http.Post("http://"+e.Addr()+"/v1/responses", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	_ = e.Stop()

	select {
	case rec := <-recs:
		return string(raw), rec
	case <-time.After(2 * time.Second):
		t.Fatal("没有收到请求记录")
		return "", RequestRecord{}
	}
}

// recSink 收集请求记录与日志（日志用于失败时定位）。
type recSink struct {
	ch   chan RequestRecord
	mu   sync.Mutex
	logs []string
}

func (s *recSink) Log(level, msg string) {
	s.mu.Lock()
	s.logs = append(s.logs, level+": "+msg)
	s.mu.Unlock()
}

func (s *recSink) Request(rec RequestRecord) { s.ch <- rec }

func (s *recSink) dump() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.logs...)
}

func waitRunning(t *testing.T, e *Engine, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if e.Running() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("引擎在 %s 内未启动", d)
}
