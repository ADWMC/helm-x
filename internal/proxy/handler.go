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
	// 控制台实时流：不在 /v1 下，直接由本进程处理，不转发上游
	if r.URL.Path == livePath {
		e.serveLive(w, r)
		return
	}

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
	// 观测：入站元信息 + 原始报文（改写前），供与 02.upstream.body 对照
	e.capMeta(&rec, r.Header.Get("User-Agent"), r.Header.Get("Content-Type"))
	e.capPut(rec.ID, "01.request.body", body)

	defer func() {
		rec.DurationMs = time.Since(start).Milliseconds()
		e.reqs.Add(1)
		e.countClass(rec.Class)
		e.finishObserve(rec)
		e.emit(rec)
	}()

	ctx := r.Context()

	// 干跑：只计算，不发上游
	if e.opts.DryRun {
		e.dryRun(w, r, body, &rec)
		return
	}

	// UA 兜底：入站没有 User-Agent 时补配置值（空则不补 —— newRequest
	// 会显式留空，绝不泄漏 Go 默认 UA 给中转站）。
	if r.Header.Get("User-Agent") == "" {
		if ua := e.fallbackUserAgent(); ua != "" {
			r.Header.Set("User-Agent", ua)
		}
	}

	// ── 1. Normalize ──
	view, perr := ParseRequest(r.URL.Path, body)
	canTransform := perr == nil && !e.opts.Passthrough
	if perr != nil {
		e.logf("warn", "proxy: 请求体无法解析，原样透传: %v", perr)
	}

	// 会话键补全：codex 不发 Session-Id/Thread-Id 头，退化为首条用户消息
	// 指纹（否则所有请求共用空桶，注入频率沦全局 1/N —— 2026-10-05 实测）。
	if rec.SessionID == "" && view != nil {
		rec.SessionID = view.SessionFingerprint()
	}

	outBody := body

	// ── 2. Inject ──
	// 注入频率：每 N 次请求注 1 次（按会话计数）。
	// shouldInject 只在"可改写且有指令"时才被调用，其余请求不占名额；
	// 注入失败也不占名额 —— markInjected 只在成功后调用，下次请求重试。
	// 客户端是否自带系统级 instructions —— 决定这一轮的系统提示词"是谁写的"。
	// replace-base 会整段覆盖它；insert/append 会叠加。纯观测（见 instructionsSource）。
	_, clientHadInstructions := view.Instructions()

	if canTransform {
		instruction := e.instruction()
		if instruction != "" && e.shouldInject(rec.SessionID) {
			mode := e.deployMode()
			var injected []byte
			var ok bool
			var ierr error
			// 三档语义见 deploy.go：insert 走 input[]，append/replace-base 走顶层 instructions。
			if mode == DeployAppend || mode == DeployReplaceBase {
				injected, ok, ierr = view.InjectInstructions(instruction, mode)
			} else {
				injected, ok, ierr = view.InjectSystemAt(instruction, e.opts.InjectTail)
			}
			if ierr == nil && ok {
				outBody = injected
				rec.Injected = true
				rec.Deploy = string(mode)
				rec.InstructionsSource = instructionsSource(true, string(mode), clientHadInstructions)
				e.markInjected(rec.SessionID)
			} else if ierr != nil {
				// 结构不支持该语义：原样透传并留可见记录（INV-9），不猜
				e.logf("warn", "proxy: 注入失败（deploy=%s），原样透传: %v", mode, ierr)
			}
		}
	}

	// ── 2.5 Transforms（codebook / encode）──
	// 与提示词注入分开：注入写 instructions/system 位，改写动 user 明文本身。
	// encode 与 codebook 互斥，encode 优先（见 transform.go）。
	if canTransform {
		outBody = e.applyTransforms(outBody, &rec)
	}
	// 未注入但客户端自带 instructions：如实标注来源为 client（passthrough / 未命中时）
	if !rec.Injected && clientHadInstructions {
		rec.InstructionsSource = instructionsSource(false, "", true)
	}

	// 观测：实际发往上游的报文（注入 + 改写后）
	e.capPut(rec.ID, "02.upstream.body", outBody)

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

// deployMode 返回当前部署语义；未配置时按 insert（历史行为）。
func (e *Engine) deployMode() DeployMode {
	if e.opts.Config == nil {
		return DeployInsert
	}
	return ParseDeployMode(e.opts.Config.Deploy())
}

// fallbackUserAgent 返回 UA 兜底值（空 = 不补）。
func (e *Engine) fallbackUserAgent() string {
	if e.opts.Config == nil {
		return ""
	}
	return e.opts.Config.FallbackUserAgent()
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
	// CLI 显式指定的策略优先：用户敲了 --no-retry，配置里的开关不能把它吃掉。
	if e.opts.RetryOverride != nil {
		return *e.opts.RetryOverride
	}
	if e.opts.Config == nil {
		return e.opts.Retry
	}
	return e.opts.Config.Retry()
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
			Status:        attempt.Status,
			Header:        attempt.Header,
			Body:          attempt.Body,
			Complete:      attempt.Complete,
			IsRefusal:     isRefusal,
			IsFlagged:     classifyIsFlagged,
			IsPolicyBlock: classifyIsPolicyBlock,
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
	if canTransform && view != nil && (st.Class == ClassRefused || st.Class == ClassFlagged || st.Class == ClassCyberPolicy) {
		userMsg, _ := view.LastUserMessage()
		out := e.remedy(ctx, st, view, in, remedyDeps{
			upstream: e.upstream,
			engine:   e,
			tamper:   e.tamper(),
			retry:    e.retryOpts(),

			injectedPrompt: e.injectedPrompt(),
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
	if st.Class == ClassUpstreamFailed && len(strings.TrimSpace(string(finalBody))) == 0 {
		// 上游彻底失败且无内容：给出结构化错误，而不是空响应
		// （2026-10 audit：旧实现在这里退化成 origBody[:0] —— 一个零长度切片，
		//  客户端只会看到 502 + 空 body，连"发生了什么"都读不到）
		finalBody = []byte(`{"error":{"message":"上游请求失败，请重试。","type":"upstream_error","code":"upstream_response_error"}}`)
	}
	// 合成错误体时必须同时改状态码（2026-10-28 体验审查）：
	// 旧实现只换 body 不改 status，上游 200+空体会变成「HTTP 200 + 错误 JSON」，
	// 客户端无法区分成功与失败 —— 状态码撒谎比没有错误体更糟。
	if st.Class == ClassUpstreamFailed || st.Class == ClassMalformed {
		if st.Status == 0 || (st.Status >= 200 && st.Status < 300) {
			st.Status = http.StatusBadGateway
		}
	}

	ct := "application/json"
	if st.SSE {
		ct = "text/event-stream; charset=utf-8"
	}
	rec.OutBytes = len(finalBody)
	// 观测：上游响应正文（补救后的最终形态）
	e.capPut(rec.ID, "03.response.body", finalBody)

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
