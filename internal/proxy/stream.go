package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ADWMC/helm-x/internal/protocol/sse"
)

// handleStream 处理流式请求。
//
// 与旧版的差别（2026-10 audit）：
//   - 流式也走重试（PostStreamRetry），与非流式对齐
//   - 窗口内命中拒答**真的**走补救（旧版只打了个 note，用户看不到任何变化）
//   - 首字节前的响应头不再提前写死，补救才能改写状态码与正文
func (e *Engine) handleStream(ctx context.Context, w http.ResponseWriter, r *http.Request,
	outBody, origBody []byte, view *RequestView, canTransform bool, rec *RequestRecord) {

	resp, hits, err := e.upstream.PostStreamRetry(ctx, r.URL.Path, outBody, r.Header, e.retryOpts(),
		func(n int, reason string) {
			e.logf("info", "proxy: 流式上游重试 %d/%d（%s）", n, e.retryOpts().MaxRetries, reason)
		})
	rec.Upstream += hits
	if err != nil {
		e.logf("error", "proxy: 流式上游请求失败: %v", err)
		rec.Class = ClassUpstreamFailed
		rec.Action = ActionNone
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"上游连接失败，请重试。","type":"upstream_error","code":"upstream_connect_error"}}`))
		return
	}
	defer resp.Body.Close()

	// 上游直接返回非 2xx：走缓冲路径（错误体通常很小）
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		tm := e.tamper()
		var isRefusal func(string) bool
		if tm != nil {
			isRefusal = tm.IsRefusal
		}
		st := Classify(ClassifyInput{
			Status:        resp.StatusCode,
			Header:        resp.Header,
			Body:          errBody,
			Complete:      true,
			IsRefusal:     isRefusal,
			IsFlagged:     classifyIsFlagged,
			IsPolicyBlock: classifyIsPolicyBlock,
		})
		if canTransform && view != nil && (st.Class == ClassRefused || st.Class == ClassFlagged) {
			userMsg, _ := view.LastUserMessage()
			out := e.remedy(ctx, st, view, r.Header, remedyDeps{
				upstream: e.upstream, engine: e, tamper: tm, rewriter: e.rewriter(),
				retry: e.retryOpts(), streamRetry: true,
			}, userMsg)
			st = out.State
			rec.Action = out.Action
			rec.Upstream += out.UpstreamHits
			if len(out.Notes) > 0 {
				rec.Note = strings.Join(out.Notes, "; ")
			}
		}
		rec.Class = st.Class
		rec.OutBytes = len(st.Body())
		e.writeResponse(w, st)
		return
	}

	e.pumpStream(ctx, w, r, resp, view, canTransform, rec)
}

// pumpStream 以窗口为界转发 SSE，并在窗口内做拒答判定。
//
// 窗口语义（docs/PLAN.md §5.5）：
//   - 窗口内命中拒答 → 丢弃缓冲、走补救阶梯，客户端**看不到**那次拒答
//   - 窗口结束（字节数或时间）→ 写响应头 + Flush，之后纯转发（O(1) 内存）
//
// 残留限制（诚实记录）：窗口计时只在有数据流动时前进 —— 上游长时间不吐
// 第一个字节时，Read 会阻塞，计时器无法抢跑。这与旧版行为一致，且不改变
// 首字节延迟（本来也要等第一个字节）。
func (e *Engine) pumpStream(ctx context.Context, w http.ResponseWriter, r *http.Request,
	resp *http.Response, view *RequestView, canTransform bool, rec *RequestRecord) {

	flusher, _ := w.(http.Flusher)
	rec.Action = ActionNone // 流式默认不做补救；命中且未提交才会被改写
	windowBytes := e.opts.StreamWindowBytes
	if windowBytes <= 0 {
		windowBytes = 2048
	}
	deadline := time.Now().Add(time.Duration(e.opts.StreamWindowMs) * time.Millisecond)

	var buffered []byte
	committed := false // 响应头是否已写（写了就不能再补救）
	parser := &sse.Parser{}
	var acc strings.Builder
	sawRefusal := false
	truncated := false

	writeHeader := func() {
		if committed {
			return
		}
		ct := resp.Header.Get("Content-Type")
		if ct == "" {
			ct = "text/event-stream; charset=utf-8"
		}
		w.Header().Set("Content-Type", ct)
		w.WriteHeader(resp.StatusCode)
		committed = true
	}

	flushBuffered := func() error {
		writeHeader()
		if len(buffered) > 0 {
			n, err := w.Write(buffered)
			rec.OutBytes += n
			buffered = buffered[:0]
			if flusher != nil {
				flusher.Flush()
			}
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}

	buf := make([]byte, 32<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if committed {
				wn, werr := w.Write(chunk)
				rec.OutBytes += wn
				if flusher != nil {
					flusher.Flush()
				}
				if werr != nil {
					rec.Class = ClassUpstreamFailed
					rec.Note = "写客户端失败: " + werr.Error()
					return
				}
			} else {
				// 窗口到期或缓冲够大：先提交，再处理这一块
				if len(buffered) >= windowBytes || time.Now().After(deadline) {
					if err := flushBuffered(); err != nil {
						rec.Class = ClassUpstreamFailed
						rec.Note = "写客户端失败: " + err.Error()
						return
					}
				}
				buffered = append(buffered, chunk...)
				// 判定与转发是两件事：
				//   - 解析/累计正文一直做，用于如实记录"这条流是拒绝"
				//   - 补救只在**尚未提交**、且拒绝出现在窗口内的文字上时做 ——
				//     一旦内容已经写给客户端，再补救就是重复投递
				if canTransform && view != nil {
					for _, ev := range parser.Feed(chunk) {
						acc.WriteString(deltaText(ev))
					}
					if !sawRefusal {
						if tm := e.tamper(); tm != nil && tm.IsRefusal(acc.String()) {
							sawRefusal = true
							if !committed && acc.Len() <= windowBytes {
								// 丢弃缓冲、走补救：响应头还没写，状态码与正文
								// 都还能改 —— 用户全程看不到这次拒答
								_ = resp.Body.Close()
								e.remedyStreaming(ctx, w, r, view, resp.StatusCode, buffered, rec)
								return
							}
						}
					}
				}
				if !committed && len(buffered) >= windowBytes {
					if err := flushBuffered(); err != nil {
						rec.Class = ClassUpstreamFailed
						rec.Note = "写客户端失败: " + err.Error()
						return
					}
				}
			}
		}
		if rerr != nil {
			if !errors.Is(rerr, io.EOF) {
				truncated = true
				rec.Note = "上游流中断: " + rerr.Error()
			}
			break
		}
	}

	if !committed {
		if err := flushBuffered(); err != nil {
			rec.Class = ClassUpstreamFailed
			rec.Note = "写客户端失败: " + err.Error()
			return
		}
	}

	switch {
	case truncated:
		// 半截流不是 Healthy：日志与 UI 都必须看出来上游断了
		rec.Class = ClassUpstreamFailed
	case sawRefusal:
		// 内容已经转发出去，不能再补救（否则重复投递），但如实记为拒绝
		rec.Class = ClassRefused
		if rec.Note == "" {
			rec.Note = "流式拒绝：已转发原文（INV-8 不删改）"
		}
	default:
		rec.Class = ClassHealthy
	}
	e.logf("info", "proxy: 流式 %s %dB→%dB class=%s %dms",
		rec.Path, rec.InBytes, rec.OutBytes, rec.Class, time.Since(rec.TS).Milliseconds())
}

// remedyStreaming 对流式窗口内命中的拒答执行补救阶梯。
func (e *Engine) remedyStreaming(ctx context.Context, w http.ResponseWriter, r *http.Request,
	view *RequestView, status int, buffered []byte, rec *RequestRecord) {

	st := ResponseState{
		Class:    ClassRefused,
		Status:   status,
		SSE:      true,
		Complete: false,
		rawBody:  append([]byte(nil), buffered...),
		Text:     extractSSEText(buffered),
		Reason:   "流式窗口内命中拒绝规则",
	}
	userMsg, _ := view.LastUserMessage()
	out := e.remedy(ctx, st, view, r.Header, remedyDeps{
		upstream: e.upstream, engine: e, tamper: e.tamper(), rewriter: e.rewriter(),
		retry: e.retryOpts(), streamRetry: true,
	}, userMsg)

	rec.Class = out.State.Class
	rec.Action = out.Action
	rec.Upstream += out.UpstreamHits
	if len(out.Notes) > 0 {
		rec.Note = strings.Join(out.Notes, "; ")
	}
	e.writeResponse(w, out.State)
	e.logf("info", "proxy: 流式补救 %s class=%s action=%s %dms",
		rec.Path, rec.Class, rec.Action, time.Since(rec.TS).Milliseconds())
}

// deltaText 取一个 SSE 事件里的 delta 文本（增量事件才有，其他返回空串）。
func deltaText(ev sse.Event) string {
	data := strings.TrimSpace(ev.Data)
	if data == "" || data == "[DONE]" {
		return ""
	}
	var m struct {
		Delta string `json:"delta"`
	}
	if json.Unmarshal([]byte(data), &m) != nil {
		return ""
	}
	return m.Delta
}
