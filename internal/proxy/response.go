package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ADWMC/helm-x/internal/protocol/sse"
)

// Class 是一次请求的最终判定。前端 Verdict 类型与之一一对应
// （见 frontend/src/composables/useVerdictStyle.ts，改动需同步）。
type Class string

const (
	ClassHealthy        Class = "Healthy"
	ClassRefused        Class = "Refused"
	ClassFlagged        Class = "Flagged"
	ClassUpstreamFailed Class = "UpstreamFailed"
	ClassMalformed      Class = "Malformed"
	ClassUnresolved     Class = "Unresolved"
)

// ResponseState 是对上游响应的判定结果。
//
// 设计要点（docs/PLAN.md §5.5）：判定顺序即语义。
// 特别是 **Refused 只可能在 2xx 上成立** —— 这是修 P8 语义不一致的关键：
// 旧版对"重试后是否仍被拒绝"扫整个响应体，与非流式路径的判定规则不同，
// 导致同一份内容在两处结论不一致。
type ResponseState struct {
	Class    Class
	Status   int
	SSE      bool
	Complete bool // 上游是否完整读完

	// Text 是用于判定的文本：
	//   非流式 → 从 JSON 中提取的正文
	// 流式   → 各事件 delta 拼接结果
	Text string

	// Reason 是人读的判定原因，写入日志与事件。
	Reason string

	// rawBody 保留原始字节，供补救阶段改写。
	rawBody []byte
}

// Body 返回原始响应字节。
func (s ResponseState) Body() []byte { return s.rawBody }

// ClassifyInput 是判定所需的最小输入，便于表驱动测试。
type ClassifyInput struct {
	Status    int
	Header    http.Header
	Body      []byte
	Complete  bool
	IsRefusal func(string) bool // 拒绝规则匹配；nil 表示不判拒绝
	IsFlagged func(int, string) bool
}

// Classify 按固定顺序判定响应类别。
//
// 顺序（不可调换）：
//  1. !Complete                    → UpstreamFailed
//  2. status ∉ 2xx                 → Flagged（命中安全标记）否则 UpstreamFailed
//  3. body 全空白                  → UpstreamFailed
//  4. body 不是合法 JSON/SSE       → Malformed
//  5. Text 命中拒绝规则            → Refused
//  6. 其余                         → Healthy
func Classify(in ClassifyInput) ResponseState {
	st := ResponseState{
		Status:   in.Status,
		Complete: in.Complete,
		rawBody:  in.Body,
	}

	// 1. 上游没读完：连接中断/超时。绝不进 TAMPER。
	if !in.Complete {
		st.Class = ClassUpstreamFailed
		st.Reason = "上游响应不完整"
		return st
	}

	isSSE := isSSEContent(in.Header, in.Body)
	st.SSE = isSSE

	// 2. 非 2xx：先看是不是安全标记，否则算上游失败。
	//    注意：这里不会产生 Refused —— 拒绝只在成功的响应里才有意义。
	if in.Status < 200 || in.Status >= 300 {
		text := extractText(in.Body, isSSE)
		st.Text = text
		if in.IsFlagged != nil && in.IsFlagged(in.Status, string(in.Body)) {
			st.Class = ClassFlagged
			st.Reason = "上游安全标记"
			return st
		}
		st.Class = ClassUpstreamFailed
		st.Reason = "上游 HTTP " + itoa(in.Status)
		return st
	}

	// 3. 空响应等同失败。
	if len(bytes.TrimSpace(in.Body)) == 0 {
		st.Class = ClassUpstreamFailed
		st.Reason = "上游返回空响应"
		return st
	}

	// 4. 形态不认识：原样透传，不猜（INV-9）。
	if !isWellFormed(in.Body, isSSE) {
		st.Class = ClassMalformed
		st.Reason = "响应形态无法解析"
		return st
	}

	// 5. 提取正文并做拒绝判定。
	st.Text = extractText(in.Body, isSSE)
	if in.IsRefusal != nil && st.Text != "" && in.IsRefusal(st.Text) {
		st.Class = ClassRefused
		st.Reason = "命中拒绝规则"
		return st
	}

	st.Class = ClassHealthy
	return st
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// isSSEContent 判断响应是否为 SSE 流。
// 优先看 Content-Type（权威），其次看 body 是否以 data:/event: 开头。
func isSSEContent(h http.Header, body []byte) bool {
	if h != nil {
		ct := strings.ToLower(h.Get("Content-Type"))
		if strings.Contains(ct, "text/event-stream") {
			return true
		}
	}
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	return bytes.HasPrefix(trimmed, []byte("data:")) || bytes.HasPrefix(trimmed, []byte("event:"))
}

// isWellFormed 判断响应体是否是认识的形态。
func isWellFormed(body []byte, isSSE bool) bool {
	if isSSE {
		return true // SSE 只要能解析出事件即可，见 extractText
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return false
	}
	if trimmed[0] != '{' && trimmed[0] != '[' {
		return false
	}
	return json.Valid(trimmed)
}

// extractText 从响应中提取用于判定的正文。
//
// 非流式：优先 output[].content[].text，其次 output_text / text 字段。
// 流式：  拼接各事件的 delta / text。
//
// 提取失败返回空串 —— 上层据此跳过拒绝判定（宁可漏判也不误判）。
func extractText(body []byte, isSSE bool) string {
	if isSSE {
		return extractSSEText(body)
	}
	return extractJSONText(body)
}

func extractSSEText(body []byte) string {
	p := &sse.Parser{}
	events := p.Feed(body)
	events = append(events, p.Flush()...)

	var sb strings.Builder
	for _, ev := range events {
		data := strings.TrimSpace(ev.Data)
		if data == "" || data == "[DONE]" {
			continue
		}
		var m map[string]any
		if json.Unmarshal([]byte(data), &m) != nil {
			continue
		}
		// delta 优先（增量事件），其次 text（完成事件会重复内容，取 delta 已够）
		if d, ok := m["delta"].(string); ok {
			sb.WriteString(d)
		}
	}
	return sb.String()
}

func extractJSONText(body []byte) string {
	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		return ""
	}
	return textFromObject(m)
}

// textFromObject 按 Responses API 的层级取正文。
func textFromObject(m map[string]any) string {
	var sb strings.Builder

	// output[].content[].text
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
				if t, ok := cm["text"].(string); ok {
					sb.WriteString(t)
				}
			}
		}
	}
	if sb.Len() > 0 {
		return sb.String()
	}

	// 退化：顶层 output_text / text
	if t, ok := m["output_text"].(string); ok {
		return t
	}
	if t, ok := m["text"].(string); ok {
		return t
	}

	// 错误响应：把 message 也当正文，便于安全标记判定
	if e, ok := m["error"].(map[string]any); ok {
		if msg, ok := e["message"].(string); ok {
			return msg
		}
	}
	return ""
}
