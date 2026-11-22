// live.go 是给控制台用的**实时请求流**（SSE）。
//
// 为什么不用 Wails 的自定义事件（2026-11-22 实测）：`app.Event.Emit` 推到前端的
// 事件在本机 WebView2 下一条都到不了（自检页因此永远停在"执行中"，请求列表只能
// 5 秒轮询）。代理本身就是个 HTTP 服务，开一条 loopback 上的 SSE 通道是本地行为、
// 完全可控，不依赖框架的事件桥。
//
// 设计约束：
//   - 只在回环地址上提供（代理只监听 127.0.0.1，见 config.ListenAddress）；
//   - 只读：客户端连上后只能收，不能改任何状态；
//   - 慢客户端直接丢消息（缓冲满即弃），绝不阻塞转发主链路 —— 这是旁路。
package proxy

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// livePath 是控制台订阅用的路径。不在 /v1 下，因此不会被当成上游请求转发。
const livePath = "/__helmx/events"

// liveHub 维护订阅者集合。
type liveHub struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

func newLiveHub() *liveHub { return &liveHub{subs: map[chan []byte]struct{}{}} }

// publish 把一条记录广播给所有订阅者。缓冲满就跳过 —— 宁可丢一帧，不拖慢代理。
func (h *liveHub) publish(rec RequestRecord) {
	if h == nil {
		return
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- b:
		default: // 慢客户端：丢这一条
		}
	}
}

// subscribe 注册一个订阅者，返回取消函数。
func (h *liveHub) subscribe() (chan []byte, func()) {
	ch := make(chan []byte, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

// serveLive 处理 SSE 订阅：先回放最近 N 条（让刚打开页面的人有上下文），再持续推送。
func (e *Engine) serveLive(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	// UI 跑在 wails.localhost，代理在 127.0.0.1 —— 跨源，只读接口放行。
	h.Set("Access-Control-Allow-Origin", "*")

	ch, cancel := e.live.subscribe()
	defer cancel()

	// 回放：把订阅前已有的最近记录补一遍（前端按 id 去重）
	for _, rec := range e.recentSnapshot() {
		if b, err := json.Marshal(rec); err == nil {
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(b)
			_, _ = w.Write([]byte("\n\n"))
		}
	}
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case b, ok := <-ch:
			if !ok {
				return
			}
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(b)
			_, _ = w.Write([]byte("\n\n"))
			flusher.Flush()
		case <-heartbeat.C:
			// 注释行心跳：防止中间层按空闲断开
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		}
	}
}

// remember 把记录存进最近环（供 SSE 首订阅回放），容量 200。
func (e *Engine) remember(rec RequestRecord) {
	const cap = 200
	e.mu.Lock()
	e.recent = append(e.recent, rec)
	if len(e.recent) > cap {
		e.recent = e.recent[len(e.recent)-cap:]
	}
	e.mu.Unlock()
}

// recentSnapshot 复制最近记录（读锁内复制，避免与写入竞争）。
func (e *Engine) recentSnapshot() []RequestRecord {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]RequestRecord, len(e.recent))
	copy(out, e.recent)
	return out
}
