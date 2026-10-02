package proxy

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/ADWMC/helm-x/internal/protocol/sse"
)

func (e *Engine) handleStream(ctx context.Context, w http.ResponseWriter, r *http.Request,
	outBody, origBody []byte, view *RequestView, canTransform bool, rec *RequestRecord) {

	resp, err := e.upstream.PostStream(ctx, r.URL.Path, outBody, r.Header)
	if err != nil {
		e.logf("error", "proxy: 流式上游请求失败: %v", err)
		rec.Class = ClassUpstreamFailed
		rec.Action = ActionNone
		rec.Upstream = 1
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"上游连接失败，请重试。","type":"upstream_error","code":"upstream_connect_error"}}`))
		return
	}
	defer resp.Body.Close()
	rec.Upstream = 1

	// 上游直接返回非 2xx：走缓冲路径（错误体通常很小）
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		tm := e.tamper()
		var isRefusal func(string) bool
		if tm != nil {
			isRefusal = tm.IsRefusal
		}
		st := Classify(ClassifyInput{
			Status:    resp.StatusCode,
			Header:    resp.Header,
			Body:      errBody,
			Complete:  true,
			IsRefusal: isRefusal,
			IsFlagged: classifyIsFlagged,
		})
		if canTransform && view != nil && (st.Class == ClassRefused || st.Class == ClassFlagged) {
			userMsg, _ := view.LastUserMessage()
			out := e.remedy(ctx, st, view, r.Header, remedyDeps{
				upstream: e.upstream, tamper: e.tamper(), rewriter: e.rewriter(), retry: e.retryOpts(),
			}, userMsg)
			st = out.State
			rec.Action = out.Action
			rec.Upstream += out.UpstreamHits
		}
		rec.Class = st.Class
		e.writeResponse(w, st)
		return
	}

	e.pumpStream(w, resp, canTransform, rec)
}

// pumpStream 以窗口为界转发 SSE。
func (e *Engine) pumpStream(w http.ResponseWriter, resp *http.Response, canTransform bool, rec *RequestRecord) {
	flusher, _ := w.(http.Flusher)

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "text/event-stream; charset=utf-8"
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(resp.StatusCode)
	if flusher != nil {
		flusher.Flush()
	}

	windowBytes := e.opts.StreamWindowBytes
	deadline := time.Now().Add(time.Duration(e.opts.StreamWindowMs) * time.Millisecond)

	var buffered []byte
	flushed := false
	parser := &sse.Parser{}
	var sawRefusal bool

	buf := make([]byte, 32<<10)
	for {
		if !flushed {
			// 窗口内：先看是不是超时
			if time.Now().After(deadline) || len(buffered) >= windowBytes {
				n, err := flush(&buffered, w, flusher)
				rec.OutBytes += n
				if err != nil {
					rec.Class = ClassUpstreamFailed
					return
				}
				flushed = true
				continue
			}
		}

		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if !flushed {
				buffered = append(buffered, chunk...)
				// 窗口内做一次判定尝试
				if canTransform {
					for _, ev := range parser.Feed(chunk) {
						_ = ev
					}
					if text := parserDeltaText(append(append([]byte(nil), buffered...), nil...)); text != "" {
						tm := e.tamper()
						if tm != nil && tm.IsRefusal(text) {
							sawRefusal = true
						}
					}
				}
				if len(buffered) >= windowBytes {
					n, err := flush(&buffered, w, flusher)
					rec.OutBytes += n
					if err != nil {
						rec.Class = ClassUpstreamFailed
						return
					}
					flushed = true
				}
				continue
			}
			wn, werr := w.Write(chunk)
			rec.OutBytes += wn
			if werr != nil {
				rec.Class = ClassUpstreamFailed
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			break
		}
	}

	// 流结束：若始终没 flush（内容很短），在此 flush
	if !flushed {
		n, _ := flush(&buffered, w, flusher)
		rec.OutBytes += n
		flushed = true
	}

	if sawRefusal {
		rec.Class = ClassRefused
		rec.Note = "流式拒绝：已转发原文（INV-8 不删改）"
	} else {
		rec.Class = ClassHealthy
	}
	e.logf("info", "proxy: 流式 %s %dB→%dB class=%s %dms",
		rec.Path, rec.InBytes, rec.OutBytes, rec.Class, time.Since(rec.TS).Milliseconds())
}

// flush 把缓冲写出并返回实际写入的字节数。
func flush(buffered *[]byte, w http.ResponseWriter, f http.Flusher) (int, error) {
	if len(*buffered) == 0 {
		if f != nil {
			f.Flush()
		}
		return 0, nil
	}
	n, err := w.Write(*buffered)
	*buffered = (*buffered)[:0]
	if f != nil {
		f.Flush()
	}
	return n, err
}

// parserDeltaText 从已缓冲的 SSE 字节里提取 delta 文本，用于窗口内判定。
func parserDeltaText(body []byte) string {
	return extractSSEText(body)
}
