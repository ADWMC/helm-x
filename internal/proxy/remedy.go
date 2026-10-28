package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ADWMC/helm-x/internal/protocol/jsonwalk"
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

// retryPost 是一次"重发"的抽象。
//
// 非流式走 PostWithRetry（完整读入）；流式必须用流式重发 —— codex 发
// stream:true 时期待 SSE，把 JSON 塞回去会让客户端解析失败。
// 返回的 Attempt 里 Body 是完整的 SSE 字节，Classify 会据 Content-Type
// 把 SSE 标出来，writeResponse 据此选择正确的 Content-Type 转发。
type retryPost func(ctx context.Context, path string, body []byte, in http.Header, opts RetryOptions) Attempt

// remedyDeps 是补救所需的外部能力，便于测试注入替身。
type remedyDeps struct {
	upstream *Upstream
	engine   *Engine
	tamper   TamperMatcher
	rewriter Rewriter
	retry    RetryOptions
	// streamRetry 为 true 时用流式 clean session 重发（补救流式请求专用）
	streamRetry bool
	// post 覆盖默认发送实现；nil 表示用 upstream.PostWithRetry
	post retryPost
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

// noopTamper 在未配置规则时兜底，避免 nil 接口解引用 panic。
type noopTamper struct{}

func (noopTamper) IsRefusal(string) bool  { return false }
func (noopTamper) Attach(s string) string { return s }
func (noopTamper) Marker() string         { return "" }

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

	tm := deps.tamper
	if tm == nil {
		tm = noopTamper{}
	}

	// ── 0 级：网络安全策略拦截是**终局**，一级都不走 ──
	//
	// 依据（2026-10-28）：codex 把 cyber_policy 归为不可重试错误；
	// yetone/magpie 的实测（efb7cb11d）证明内容拒绝跟着请求走、换会话无用，
	// 原样把 400 交给客户端之后，下一个请求在同一账号立即可用。
	// 我们此前在这里做 clean-session 重发 —— 内容没变，必然再拒，白烧额度，
	// 还把 codex 自己的 safety_recourse 通路挡死。
	if st.Class == ClassCyberPolicy {
		out.Action = ActionPassThrough
		out.State = st // 保持 CyberPolicy 类别，前端才能单独显示
		out.Notes = append(out.Notes, "网络安全策略拦截：终局判定，不重试、不改写，原样透传")
		return out
	}

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
		cleanBody, err := view.BuildCleanSessionMode(msg, deps.streamRetry)
		if err == nil && len(cleanBody) > 0 {
			a := e.postForRetry(ctx, deps, view.Path, cleanBody, in)
			out.UpstreamHits++

			if a.Complete && a.Status >= 200 && a.Status < 300 && len(a.Body) > 0 {
				retryState := Classify(ClassifyInput{
					Status:        a.Status,
					Header:        a.Header,
					Body:          a.Body,
					Complete:      true,
					IsRefusal:     tm.IsRefusal,
					IsFlagged:     classifyIsFlagged,
					IsPolicyBlock: classifyIsPolicyBlock,
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
	if attached, ok := attachMarkerToState(st, tm); ok {
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

// postForRetry 执行第 1 级重发：默认 PostWithRetry，流式补救走流式通道。
func (e *Engine) postForRetry(ctx context.Context, deps remedyDeps, path string, body []byte, in http.Header) Attempt {
	if deps.post != nil {
		return deps.post(ctx, path, body, in, deps.retry)
	}
	if deps.streamRetry {
		return deps.upstream.PostStreamSync(ctx, path, body, in, deps.retry)
	}
	return deps.upstream.PostWithRetry(ctx, path, body, in, deps.retry, nil)
}

// classifyIsPolicyBlock 判定上游网络安全策略拦截（cyber_policy）。
//
// 两类来源都要认：
//  1. 非 2xx 的响应体里带 cyber 标记（400 + safety filter: cyber_policy）；
//  2. 200 的流里用 `response.failed` / safety_filter 下发终局（HTTP 状态看不出来）。
//
// 只在**上游响应**上判，不看我们的请求体；命中即终局，不进补救阶梯。
func classifyIsPolicyBlock(status int, body string) bool {
	if !containsAny(body, policyMarkers) {
		return false
	}
	if status >= 400 && status < 500 {
		return true
	}
	// 2xx：必须是终局事件才算，避免模型正文里顺口提到这些词被误判
	return strings.Contains(body, "response.failed") ||
		strings.Contains(body, "safety_filter") ||
		strings.Contains(body, `"code":"cyber_policy"`) ||
		strings.Contains(body, `"code": "cyber_policy"`)
}

// policyMarkers 是上游 cyber 拦截的标记词。与 codex 的 ApiError::CyberPolicy 判定同源。
var policyMarkers = []string{
	"cyber_policy",
	"flagged for possible cybersecurity",
	"Trusted Access for Cyber",
	"cybersecurity risk",
	"网络安全策略",
	"该会话已被网络安全策略屏蔽",
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// classifyIsFlagged 是安全标记判定，与旧版关键词保持一致（proxy.cpp:939-946）。
func classifyIsFlagged(status int, body string) bool {
	if status < 400 || status >= 500 {
		return false
	}
	return containsAny(body, policyMarkers)
}

// attachMarkerToState 把标记附加到响应正文，**保留原文**。
//
// 非流式 JSON：用 jsonwalk 做**字节级**插入/替换 —— 不重新序列化，
// 因此 key 顺序、缩进、未知字段全部原样保留（INV-4 在响应侧同样成立）。
// 流式 SSE：在正文首个 delta 前插入标记事件，原文事件全部保留。
//
// 返回 false 表示该形态无法安全附加（调用方进第 3 级原样透传）。
func attachMarkerToState(st ResponseState, t TamperMatcher) (ResponseState, bool) {
	marker := t.Marker()
	if marker == "" {
		return st, false // 没有标记就不算补救，交给 pass_through
	}
	if st.SSE {
		return attachMarkerSSE(st, marker)
	}
	return attachMarkerJSON(st, marker)
}

// attachMarkerJSON 用 jsonwalk 在原始字节上插入/替换正文。
//
// 2026-10 audit：旧实现走 json.Unmarshal → map → Marshal，
// 会把 {"content":…,"type":…} 重排成 {"type":…,"content":…}，
// 既破坏 INV-4，也让做响应签名的客户端直接失败。
func attachMarkerJSON(st ResponseState, marker string) (ResponseState, bool) {
	root, err := jsonwalk.Parse(st.rawBody)
	if err != nil {
		return st, false
	}

	// 1) 首选 output[].content[].text：取第一个非空字符串，前置标记
	if out := root.Member("output"); out != nil && out.Kind == jsonwalk.KindArray {
		for i := 0; i < out.Len(); i++ {
			content := out.Index(i).Member("content")
			if content == nil || content.Kind != jsonwalk.KindArray {
				continue
			}
			for j := 0; j < content.Len(); j++ {
				c := content.Index(j)
				if c == nil || c.Kind != jsonwalk.KindObject {
					continue
				}
				txt := c.Member("text")
				if txt == nil || txt.Kind != jsonwalk.KindString {
					continue
				}
				orig := txt.MustString()
				if orig == "" {
					continue
				}
				newBody, rerr := jsonwalk.ReplaceString(st.rawBody, txt, marker+orig)
				if rerr != nil {
					return st, false
				}
				st.rawBody = newBody
				st.Text = marker + st.Text
				return st, true
			}
		}
	}

	// 2) 退化：顶层 output_text / text
	for _, key := range []string{"output_text", "text"} {
		v := root.Member(key)
		if v == nil || v.Kind != jsonwalk.KindString || v.MustString() == "" {
			continue
		}
		newBody, rerr := jsonwalk.ReplaceString(st.rawBody, v, marker+v.MustString())
		if rerr != nil {
			return st, false
		}
		st.rawBody = newBody
		st.Text = marker + st.Text
		return st, true
	}
	return st, false
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
