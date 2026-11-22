package proxy

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 实时流的三条契约：能收到、慢客户端不阻塞、最近记录可回放。

func TestLiveHubPublishSubscribe(t *testing.T) {
	h := newLiveHub()
	ch, cancel := h.subscribe()
	defer cancel()

	h.publish(RequestRecord{ID: "req_1", Path: "/v1/responses", Class: ClassHealthy})
	select {
	case b := <-ch:
		if !strings.Contains(string(b), `"id":"req_1"`) {
			t.Errorf("收到的帧不含 id: %s", b)
		}
	case <-time.After(time.Second):
		t.Fatal("没收到 publish 的帧")
	}
}

// 订阅者缓冲满了必须丢帧而不是阻塞发布方（否则一个卡住的页面会拖慢代理）。
func TestLiveHubDropsWhenFull(t *testing.T) {
	h := newLiveHub()
	_, cancel := h.subscribe()
	defer cancel()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ { // 远超 64 的缓冲
			h.publish(RequestRecord{ID: "req_x"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publish 被慢订阅者阻塞")
	}
}

func TestRememberRingCap(t *testing.T) {
	e := &Engine{live: newLiveHub(), byClass: map[Class]uint64{}}
	for i := 0; i < 250; i++ {
		e.remember(RequestRecord{ID: "req"})
	}
	if got := len(e.recentSnapshot()); got != 200 {
		t.Errorf("回放缓冲 = %d 条, want 200", got)
	}
}

// /__helmx/events 由本进程处理：不回上游、返回 SSE 头。
func TestLivePathServedLocally(t *testing.T) {
	e, err := New(Options{Upstream: "http://127.0.0.1:1/v1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立刻取消：只要响应头，不进入流循环

	req := httptest.NewRequest("GET", livePath, nil).WithContext(ctx)
	w := httptest.NewRecorder()
	e.handle(w, req)

	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if ao := w.Header().Get("Access-Control-Allow-Origin"); ao != "*" {
		t.Errorf("缺少 CORS 头（UI 在 wails.localhost，跨源订阅）：%q", ao)
	}
}

// 非 live 路径不受影响（继续走转发管线）。
func TestNonLivePathNotIntercepted(t *testing.T) {
	e, err := New(Options{Upstream: "http://127.0.0.1:1/v1"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("GET", "/v1/models", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	e.handle(w, req)
	if ct := w.Header().Get("Content-Type"); strings.Contains(ct, "text/event-stream") {
		t.Error("普通路径被误判成实时流")
	}
}
