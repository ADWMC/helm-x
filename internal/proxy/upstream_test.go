package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// ── 转发口径（upstream.go）：除逐跳/传输头外全头透传 ──

func newUpstreamForTest(t *testing.T) *Upstream {
	t.Helper()
	u, err := NewUpstream("https://relay.example/v1")
	if err != nil {
		t.Fatalf("NewUpstream: %v", err)
	}
	return u
}

// 白名单会丢的头（UA、Cookie、自定义 X-* 等）必须原样透传。
func TestForwardPassesHeaders(t *testing.T) {
	u := newUpstreamForTest(t)
	in := http.Header{}
	in.Set("User-Agent", "codex_exec/0.159.3 (Windows 10.0.26300; x86_64)")
	in.Set("Authorization", "Bearer sk-test")
	in.Set("Cookie", "session=abc")
	in.Set("Originator", "codex_exec")
	in.Set("X-Relay-Token", "tok-123")
	in.Set("X-Codex-Turn-Metadata", "meta-1")

	req, err := u.newRequest(context.Background(), "/responses", []byte("{}"), in)
	if err != nil {
		t.Fatalf("newRequest: %v", err)
	}
	for k, want := range map[string]string{
		"User-Agent":            "codex_exec/0.159.3 (Windows 10.0.26300; x86_64)",
		"Authorization":         "Bearer sk-test",
		"Cookie":                "session=abc",
		"Originator":            "codex_exec",
		"X-Relay-Token":         "tok-123",
		"X-Codex-Turn-Metadata": "meta-1",
	} {
		if got := req.Header.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

// 逐跳头与 Accept-Encoding 不透传。
func TestForwardDropsHopHeaders(t *testing.T) {
	u := newUpstreamForTest(t)
	in := http.Header{}
	for _, k := range []string{
		"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
		"Proxy-Connection", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
		"Accept-Encoding",
	} {
		in.Set(k, "junk-value")
	}
	in.Set("Content-Length", "999")

	req, err := u.newRequest(context.Background(), "/responses", []byte("{}"), in)
	if err != nil {
		t.Fatalf("newRequest: %v", err)
	}
	for _, k := range []string{
		"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
		"Proxy-Connection", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
		"Accept-Encoding",
	} {
		if v := req.Header.Get(k); v != "" {
			t.Errorf("%s 应被剔除，实得 %q", k, v)
		}
	}
}

// 入站无 UA 时：显式留空 —— 传输层不发送该头，绝不出现 Go-http-client。
func TestForwardOmitsEmptyUA(t *testing.T) {
	u := newUpstreamForTest(t)
	req, err := u.newRequest(context.Background(), "/responses", []byte("{}"), http.Header{})
	if err != nil {
		t.Fatalf("newRequest: %v", err)
	}
	vs, ok := req.Header["User-Agent"]
	if !ok || len(vs) != 1 || vs[0] != "" {
		t.Errorf("User-Agent 应为显式空值（表示不发送），实得 %v", vs)
	}
}

// Content-Type：有则透传，无则默认 json。
func TestForwardContentType(t *testing.T) {
	u := newUpstreamForTest(t)

	in := http.Header{}
	in.Set("Content-Type", "application/json; charset=utf-8")
	req, _ := u.newRequest(context.Background(), "/responses", []byte("{}"), in)
	if got := req.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want 原样透传", got)
	}

	req2, _ := u.newRequest(context.Background(), "/responses", []byte("{}"), http.Header{})
	if got := req2.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
}

// ── UA 兜底（handler 层） ──

// 抓头假上游。
func newHeaderCapture(t *testing.T) (*httptest.Server, *headerRecorder) {
	t.Helper()
	rec := &headerRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.got = r.Header.Clone()
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"output":[]}`)
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

type headerRecorder struct {
	mu  sync.Mutex
	got http.Header
}

func (h *headerRecorder) header() http.Header {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.got
}

const uaReqBody = `{"model":"m","stream":false,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`

// 入站无 UA + 配置了兜底 → 上游看到兜底值。
func TestPipelineFallbackUA(t *testing.T) {
	srv, rec := newHeaderCapture(t)
	cfg := &testConfig{
		fallbackUA: "codex_exec/0.159.3 (Windows 10.0.26300; x86_64)",
		tamper:     &stubTamper{},
		retry:      RetryOptions{Enabled: false},
	}
	e, err := New(Options{
		Listen:   "127.0.0.1:0",
		Upstream: srv.URL + "/v1",
		Config:   cfg,
		Retry:    RetryOptions{Enabled: false},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	doRequestSession(t, e, uaReqBody, "s")
	if got := rec.header().Get("User-Agent"); got != cfg.fallbackUA {
		t.Errorf("上游 User-Agent = %q, want 兜底值 %q", got, cfg.fallbackUA)
	}
}

// 入站有 UA → 兜底不生效，原样透传。
func TestPipelineUAPassthroughWins(t *testing.T) {
	srv, rec := newHeaderCapture(t)
	cfg := &testConfig{
		fallbackUA: "FALLBACK-SHOULD-NOT-APPEAR",
		tamper:     &stubTamper{},
		retry:      RetryOptions{Enabled: false},
	}
	e, err := New(Options{
		Listen:   "127.0.0.1:0",
		Upstream: srv.URL + "/v1",
		Config:   cfg,
		Retry:    RetryOptions{Enabled: false},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var captured RequestRecord
	e.opts.Events = &captureSink{rec: &captured}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(uaReqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "codex_exec/0.159.3")
	e.handle(httptest.NewRecorder(), req)

	if got := rec.header().Get("User-Agent"); got != "codex_exec/0.159.3" {
		t.Errorf("上游 User-Agent = %q, want 原样透传", got)
	}
}

// 入站无 UA 且未配置兜底 → 上游**不带** UA（绝不是 Go-http-client）。
func TestPipelineNoUALeak(t *testing.T) {
	srv, rec := newHeaderCapture(t)
	cfg := &testConfig{tamper: &stubTamper{}, retry: RetryOptions{Enabled: false}}
	e, err := New(Options{
		Listen:   "127.0.0.1:0",
		Upstream: srv.URL + "/v1",
		Config:   cfg,
		Retry:    RetryOptions{Enabled: false},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	doRequestSession(t, e, uaReqBody, "s")
	got := rec.header().Get("User-Agent")
	if got != "" {
		t.Errorf("上游 User-Agent = %q, want 不发送（空）", got)
	}
	if strings.Contains(got, "Go-http-client") {
		t.Error("泄漏了 Go 默认 UA")
	}
}
