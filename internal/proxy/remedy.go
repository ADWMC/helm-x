package proxy

import (
	"context"
	"encoding/json"
	"net/http"
)

// Action 是补救阶梯上执行的动作。
type Action string

const (
	ActionNone         Action = "none"
	ActionRetry        Action = "retry"         // clean session（±改写）重发
	ActionAttachMarker Action = "attach_marker" // 保留原话，附加标记
	ActionPassThrough  Action = "pass_through"  // 原样返回，记 unresolved
)

// Outcome 是一次补救的结果。
type Outcome struct {
	Action Action
	State  ResponseState
	Notes  []string
	// UpstreamHits 是本次请求实际发出的上游请求数，用于 INV-2 断言
	UpstreamHits int
}

// remedyDeps 是补救所需的外部能力，便于测试注入替身。
type remedyDeps struct {
	upstream *Upstream
	engine   *Engine
	tamper   TamperMatcher
	rewriter Rewriter
	retry    RetryOptions
}

// TamperMatcher 由 tamper.Engine 满足。
type TamperMatcher interface {
	IsRefusal(text string) bool
	Attach(text string) string
	Marker() string
}

// Rewriter 是可选的语义改写器（LLM 路径）。
// 返回 (改写结果, 是否成功)。失败必须返回 false —— 上层据此直接进下一级，
// **不再重试**（INV-2：不额外消耗用户额度）。
type Rewriter interface {
	Rewrite(ctx context.Context, userMsg, refusal, conversation string) (string, bool)
}

// remedy 对 Refused/Flagged 的响应执行补救阶梯。
//
// 阶梯（docs/PLAN.md §5.5）：
//
//  1. Retry         clean session（±改写消息）重发一次
//  2. AttachMarker  保留模型原话，附加标记（INV-8）
//  3. PassThrough   原样返回 + 记 unresolved
//
// 关键不变量：
//   - INV-8 绝不删除模型原有输出：第 2 级只附加不替换
//   - INV-2 改写失败不重试：直接进下一级
//   - UpstreamFailed 永不进入本函数（由调用方保证）
func (e *Engine) remedy(ctx context.Context, st ResponseState, view *RequestView,
	in http.Header, deps remedyDeps, userMsg string) Outcome {

	out := Outcome{Action: ActionNone, State: st, UpstreamHits: 0}

	// 只有 2xx 上的 Refused，以及 Flagged，才值得补救。
	// UpstreamFailed 已经在 Classify 里被排除，这里再守一道。
	if st.Class != ClassRefused && st.Class != ClassFlagged {
		return out
	}

	// ── 第 1 级：clean session 重发 ──
	msg := userMsg
	// Flagged 时如果有改写器，先做语义改写（与旧版 cyber 分支一致）
	if st.Class == ClassFlagged && deps.rewriter != nil && userMsg != "" {
		if rewritten, ok := deps.rewriter.Rewrite(ctx, userMsg, st.Text, view.ConversationText()); ok && rewritten != "" {
			msg = rewritten
			out.Notes = append(out.Notes, "已语义改写")
		} else {
			out.Notes = append(out.Notes, "改写失败，沿用原文")
		}
	}

	if msg != "" || st.Class == ClassRefused {
		cleanBody, err := view.BuildCleanSession(msg)
		if err == nil && len(cleanBody) > 0 {
			a := deps.upstream.PostWithRetry(ctx, view.Path, cleanBody, in, deps.retry, nil)
			out.UpstreamHits++

			if a.Complete && a.Status >= 200 && a.Status < 300 && len(a.Body) > 0 {
				retryState := Classify(ClassifyInput{
					Status:    a.Status,
					Header:    a.Header,
					Body:      a.Body,
					Complete:  true,
					IsRefusal: deps.tamper.IsRefusal,
					IsFlagged: classifyIsFlagged,
				})
				// 重发成功且不再命中拒绝 → 采纳
				if retryState.Class == ClassHealthy {
					out.Action = ActionRetry
					out.State = retryState
					out.Notes = append(out.Notes, "clean session 重发成功")
					return out
				}
				// 重发仍被拒 → 用重发的内容继续走第 2 级
				st = retryState
				out.State = retryState
				out.Notes = append(out.Notes, "重发仍被拒绝")
			} else {
				out.Notes = append(out.Notes, "重发未取得有效响应")
			}
		}
	}

	// ── 第 2 级：附加标记，保留原话（INV-8）──
	if attached, ok := attachMarkerToState(st, deps.tamper); ok {
		out.Action = ActionAttachMarker
		out.State = attached
		out.Notes = append(out.Notes, "已附加标记，原文保留")
		return out
	}

	// ── 第 3 级：原样透传，明确标记为未补救 ──
	out.Action = ActionPassThrough
	out.State = st
	out.State.Class = ClassUnresolved
	out.Notes = append(out.Notes, "无法补救，原样返回")
	return out
}

// classifyIsFlagged 是安全标记判定，与旧版关键词保持一致（proxy.cpp:939-946）。
func classifyIsFlagged(status int, body string) bool {
	if status < 400 || status >= 500 {
		return false
	}
	for _, kw := range []string{
		"cyber_policy",
		"flagged for possible cybersecurity",
		"Trusted Access for Cyber",
		"cybersecurity risk",
		"网络安全策略",
		"该会话已被网络安全策略屏蔽",
	} {
		if contains(body, kw) {
			return true
		}
	}
	return false
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	n, m := len(s), len(sub)
	if m == 0 {
		return 0
	}
	for i := 0; i+m <= n; i++ {
		if s[i:i+m] == sub {
			return i
		}
	}
	return -1
}

// attachMarkerToState 把标记附加到响应正文，**保留原文**。
//
// 非流式 JSON：改写 output[].content[].text（或顶层 output_text/text）。
// 流式 SSE：在正文首个 delta 前插入标记事件 —— 原文事件全部保留。
//
// 返回 false 表示该形态无法安全附加（调用方进第 3 级原样透传）。
func attachMarkerToState(st ResponseState, t TamperMatcher) (ResponseState, bool) {
	marker := t.Marker()
	if st.SSE {
		return attachMarkerSSE(st, marker)
	}
	return attachMarkerJSON(st, marker)
}

func attachMarkerJSON(st ResponseState, marker string) (ResponseState, bool) {
	var m map[string]any
	if json.Unmarshal(st.rawBody, &m) != nil {
		return st, false
	}

	// 尝试 output[].content[].text
	modified := false
	if output, ok := m["output"].([]any); ok {
		for _, item := range output {
			im, ok := item.(map[string]any)
			if !ok {
				continue
			}
			content, ok := im["content"].([]any)
			if !ok {
				continue
			}
			for _, c := range content {
				cm, ok := c.(map[string]any)
				if !ok {
					continue
				}
				if txt, ok := cm["text"].(string); ok && txt != "" {
					cm["text"] = marker + txt
					modified = true
					break
				}
			}
			if modified {
				break
			}
		}
	}

	if !modified {
		// 退化到顶层字段
		if txt, ok := m["output_text"].(string); ok && txt != "" {
			m["output_text"] = marker + txt
			modified = true
		} else if txt, ok := m["text"].(string); ok && txt != "" {
			m["text"] = marker + txt
			modified = true
		}
	}
	if !modified {
		return st, false
	}

	out, err := json.Marshal(m)
	if err != nil {
		return st, false
	}
	st.rawBody = out
	st.Text = marker + st.Text
	return st, true
}

// attachMarkerSSE 在流的最前面插入一个携带标记的 delta 事件。
//
// 原文事件一个都不删 —— 这是流式路径下 INV-8 的实现方式。
func attachMarkerSSE(st ResponseState, marker string) (ResponseState, bool) {
	esc, err := json.Marshal(marker)
	if err != nil {
		return st, false
	}
	// 构造一个标准的 output_text.delta 事件，与实测的真实上游格式一致
	ev := "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":" +
		string(esc) + "}\n\n"

	out := make([]byte, 0, len(ev)+len(st.rawBody))
	out = append(out, ev...)
	out = append(out, st.rawBody...)
	st.rawBody = out
	st.Text = marker + st.Text
	return st, true
}
