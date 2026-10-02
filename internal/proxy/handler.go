// handler.go 是请求管线：Normalize → Inject → Send → Interpret → Remedy → Transmit。
// 流式路径在 stream.go，判定在 response.go，补救在 remedy.go。

package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func (e *Engine) handle(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rec := RequestRecord{
		ID:        fmt.Sprintf("req_%d", e.seq.Add(1)),
		TS:        start,
		Path:      r.URL.Path,
		SessionID: firstNonEmpty(r.Header.Get("Session-Id"), r.Header.Get("Thread-Id")),
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		http.Error(w, "读取请求体失败", http.StatusBadRequest)
		return
	}
	if len(body) > maxBodyBytes {
		http.Error(w, "请求体过大", http.StatusRequestEntityTooLarge)
		return
	}
	rec.InBytes = len(body)
	defer func() {
		rec.DurationMs = time.Since(start).Milliseconds()
		e.reqs.Add(1)
		e.countClass(rec.Class)
		e.emit(rec)
	}()

	ctx := r.Context()

	// 干跑：只计算，不发上游
	if e.opts.DryRun {
		e.dryRun(w, r, body, &rec)
		return
	}

	// ── 1. Normalize ──
	view, perr := ParseRequest(r.URL.Path, body)
	canTransform := perr == nil && !e.opts.Passthrough
	if perr != nil {
		e.logf("warn", "proxy: 请求体无法解析，原样透传: %v", perr)
	}

	// ── 2. Inject ──
	outBody := body
	if canTransform {
		instruction := e.instruction()
		if instruction != "" {
			injected, ok, ierr := view.InjectSystem(instruction)
			if ierr == nil && ok {
				outBody = injected
				rec.Injected = true
			}
		}
	}

	// ── 3/4/5. Send → Interpret → Remedy ──
	isStream := false
	if canTransform {
		isStream = view.Stream
	}
	if !canTransform && wantsStreamBody(body) {
		isStream = true
	}

	if isStream {
		e.handleStream(ctx, w, r, outBody, body, view, canTransform, &rec)
		return
	}

	e.handleBuffered(ctx, w, r, outBody, body, view, canTransform, &rec)
}

// instruction 返回当前要注入的指令。
func (e *Engine) instruction() string {
	if e.opts.Config == nil {
		return ""
	}
	return e.opts.Config.PromptInstruction()
}

func (e *Engine) tamper() TamperMatcher {
	if e.opts.Config == nil {
		return nil
	}
	return e.opts.Config.Tamper()
}

// retryOpts 解析本次请求应使用的重试策略。
//
// ConfigProvider 提供运行时配置（UI 改动即时生效）；未提供 Config 时用启动参数。
//
// 注意：不能拿"零值结构体"当"未设置"的判据 ——
// {Enabled:false, MaxRetries:0, DelaySeconds:0} 是一个**合法**配置
// （即"不重试"），与"没配置"无法区分。因此这里只依据 Config 是否为 nil。
func (e *Engine) retryOpts() RetryOptions {
	if e.opts.Config == nil {
		return e.opts.Retry
	}
	return e.opts.Config.Retry()
}

func (e *Engine) rewriter() Rewriter {
	if e.opts.Config == nil {
		return nil
	}
	return e.opts.Config.Rewriter()
}

// handleBuffered 处理非流式请求：完整读取后判定并补救。
func (e *Engine) handleBuffered(ctx context.Context, w http.ResponseWriter, r *http.Request,
	outBody, origBody []byte, view *RequestView, canTransform bool, rec *RequestRecord) {

	in := r.Header
	attempt := e.upstream.PostWithRetry(ctx, r.URL.Path, outBody, in, e.retryOpts(),
		func(n int, reason string) {
			e.logf("info", "proxy: 上游重试 %d/%d（%s）", n, e.retryOpts().MaxRetries, reason)
		})
	rec.Upstream = 1

	upHits := 1
	var st ResponseState
	if canTransform {
		tm := e.tamper()
		var isRefusal func(string) bool
		if tm != nil {
			isRefusal = tm.IsRefusal
		}
		st = Classify(ClassifyInput{
			Status:    attempt.Status,
			Header:    attempt.Header,
			Body:      attempt.Body,
			Complete:  attempt.Complete,
			IsRefusal: isRefusal,
			IsFlagged: classifyIsFlagged,
		})
	} else {
		st = ResponseState{
			Status:   attempt.Status,
			SSE:      false,
			Complete: attempt.Complete,
			rawBody:  attempt.Body,
			Class:    ClassHealthy,
		}
		if !attempt.Complete || attempt.Status == 0 {
			st.Class = ClassUpstreamFailed
		}
	}

	// ── Remedy ──
	if canTransform && view != nil && (st.Class == ClassRefused || st.Class == ClassFlagged) {
		userMsg, _ := view.LastUserMessage()
		out := e.remedy(ctx, st, view, in, remedyDeps{
			upstream: e.upstream,
			engine:   e,
			tamper:   e.tamper(),
			rewriter: e.rewriter(),
			retry:    e.retryOpts(),
		}, userMsg)
		st = out.State
		rec.Action = out.Action
		upHits += out.UpstreamHits
		if len(out.Notes) > 0 {
			rec.Note = strings.Join(out.Notes, "; ")
		}
	} else {
		rec.Action = ActionNone
	}
	rec.Upstream = upHits
	rec.Class = st.Class

	// ── Transmit ──
	finalBody := st.rawBody
	if st.Class == ClassUpstreamFailed && (finalBody == nil || len(strings.TrimSpace(string(finalBody))) == 0) {
		// 上游彻底失败且无内容：给出结构化错误，而不是空响应
		finalBody = []byte(`{"error":{"message":"上游请求失败，请重试。","type":"upstream_error","code":"upstream_response_error"}}`)
	}
	if finalBody == nil {
		finalBody = origBody[:0]
	}

	ct := "application/json"
	if st.SSE {
		ct = "text/event-stream; charset=utf-8"
	}
	rec.OutBytes = len(finalBody)

	status := st.Status
	if status == 0 {
		status = http.StatusBadGateway
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(status)
	_, _ = w.Write(finalBody)

	e.logf("info", "proxy: %s %s %dB→%dB class=%s action=%s %dms",
		r.Method, r.URL.Path, rec.InBytes, rec.OutBytes, st.Class, rec.Action, time.Since(rec.TS).Milliseconds())
}

// handleStream 处理流式请求：首段缓冲窗口 + 边读边转发。
//
// 窗口的作用（修 P3，docs/PLAN.md §5.5）：
//   - 窗口内若已能判定为拒绝 → 丢弃缓冲走补救，用户什么都看不到
//   - 窗口结束 → Flush 缓冲，之后纯转发（O(1) 内存）
//
// 旧版是**全缓冲**：实测首字节延迟等于生成总时长（FINDINGS A-3，1.25s）。
