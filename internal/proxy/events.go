// events.go 解析上游**私有** SSE 事件（F10，2026-11-22 迁入）。
//
// 真实上游会在流里夹带非内容事件，例如：
//
//	event: rate_limits        data: {"plan":"plus","used":12,"limit":150,...}
//	event: response.metadata  data: {"session":"...","request_id":"..."}
//
// 这些事件对转发没用（原样透传即可），但它们是**唯一**能看到限流与会话归因的
// 观测点：请求列表里没有它们的话，出现 429 时无从判断是账号额度还是中转站限流。
//
// 只摘录、不改写、不参与判定（判定仍是 response.go/remedy.go 的事）。
package proxy

import (
	"strings"

	"github.com/ADWMC/helm-x/internal/protocol/sse"
)

// maxMetaBytes 是单条事件摘录上限（防止把整段 metadata 塞进内存记录）。
const maxMetaBytes = 512

// upstreamMetaEvents 是"值得记下来"的上游私有事件名。
var upstreamMetaEvents = map[string]bool{
	"rate_limits":        true,
	"response.metadata":  true,
	"response.completed": true, // 只取里面的 usage/id，见 pickCompleted
}

// collectUpstreamEvent 把私有事件摘录进 rec.UpstreamMeta。
//
// 命名规则：同一事件多次出现时保留**第一条**（rate_limits 通常在流的开头），
// 已完成事件保留最后一条（response.completed 的 usage 在末尾才有）。
func collectUpstreamEvent(rec *RequestRecord, ev sse.Event) {
	if rec == nil || ev.Name == "" || !upstreamMetaEvents[ev.Name] {
		return
	}
	data := strings.TrimSpace(ev.Data)
	if data == "" {
		return
	}
	if ev.Name == "response.completed" {
		data = pickCompleted(data)
		if data == "" {
			return
		}
	}
	if len(data) > maxMetaBytes {
		data = data[:maxMetaBytes] + "…"
	}
	if rec.UpstreamMeta == nil {
		rec.UpstreamMeta = map[string]string{}
	}
	if ev.Name == "response.completed" || rec.UpstreamMeta[ev.Name] == "" {
		rec.UpstreamMeta[ev.Name] = data
	}
}

// pickCompleted 只保留 usage/id/status 三个字段的**值**。
//
// 为什么不全存：response.completed 的 data 可能带整段输出，存下来等于把响应
// 复制两份（捕获目录已经有 response.body）。这里只摘"额度用了多少"这类数字。
func pickCompleted(data string) string {
	var out []string
	for _, key := range []string{"id", "status", "usage"} {
		if v := jsonValueOf(data, key); v != "" {
			if len(v) > 200 {
				v = v[:200] + "…"
			}
			out = append(out, `"`+key+`":`+v)
		}
	}
	return strings.Join(out, " ")
}

// jsonValueOf 从 JSON 文本里取某个键的值（原始片段）。
//
// 手写而不是 Unmarshal：面对的是"可能被截断"的上游流数据，整体解析失败会丢掉
// 全部信息；按值边界扫描能拿到多少算多少。
func jsonValueOf(data, key string) string {
	needle := `"` + key + `"`
	i := strings.Index(data, needle)
	if i < 0 {
		return ""
	}
	j := i + len(needle)
	skip := func() {
		for j < len(data) {
			switch data[j] {
			case ' ', '\t', '\n', '\r':
				j++
			default:
				return
			}
		}
	}
	skip()
	if j >= len(data) || data[j] != ':' {
		return ""
	}
	j++
	skip()
	if j >= len(data) {
		return ""
	}

	switch data[j] {
	case '{', '[':
		open, closeCh := data[j], byte('}')
		if open == '[' {
			closeCh = ']'
		}
		depth := 0
		inStr, esc := false, false
		for k := j; k < len(data); k++ {
			c := data[k]
			switch {
			case esc:
				esc = false
			case c == '\\' && inStr:
				esc = true
			case c == '"':
				inStr = !inStr
			case inStr:
			case c == open:
				depth++
			case c == closeCh:
				depth--
				if depth == 0 {
					return data[j : k+1]
				}
			}
		}
		return data[j:] // 未闭合（截断流）：返回剩余部分
	case '"':
		for k := j + 1; k < len(data); k++ {
			if data[k] == '\\' {
				k++
				continue
			}
			if data[k] == '"' {
				return data[j : k+1]
			}
		}
		return data[j:]
	default:
		for k := j; k < len(data); k++ {
			if data[k] == ',' || data[k] == '}' || data[k] == ']' {
				return strings.TrimSpace(data[j:k])
			}
		}
		return strings.TrimSpace(data[j:])
	}
}
