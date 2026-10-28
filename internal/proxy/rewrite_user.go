package proxy

import (
	"strings"

	"github.com/ADWMC/helm-x/internal/protocol/jsonwalk"
)

// ReplaceLastUserText 把**最后一条 role=user 消息**的文本替换为 text，
// 其余字节原样保留（INV-4：不重新序列化整个请求）。
//
// 为什么不重建会话：旧实现走 BuildCleanSession，把历史与 tools 全丢掉再发。
// 那条路对"内容被拒"无效（内容没变，必然再拒），还会让 codex 失去工具能力。
// 外科式替换只改一句话，历史与工具全在 —— 与 GptBypass 的 apply_refined_prompt 同形。
//
// 返回 ok=false 表示没找到可替换的目标（例如最后一条 user 消息是 environment_context）。
func (v *RequestView) ReplaceLastUserText(text string) ([]byte, bool, error) {
	if v == nil || v.input == nil || strings.TrimSpace(text) == "" {
		return nil, false, nil
	}
	for i := v.input.Len() - 1; i >= 0; i-- {
		item := v.input.Index(i)
		if item == nil {
			continue
		}
		if role := item.Member("role"); role == nil || role.MustString() != "user" {
			continue
		}
		// 文本可能在 content 数组的某个 input_text 里；从后往前取第一个非环境上下文。
		if target := lastTextValue(item); target != nil {
			if s, err := target.String(); err == nil && strings.Contains(s, "<environment_context>") {
				continue
			}
			out, err := jsonwalk.ReplaceString(v.Body, target, text)
			if err != nil {
				return nil, false, err
			}
			return out, true, nil
		}
	}
	return nil, false, nil
}

// lastTextValue 返回消息里最后一个文本节点（string 或 content[].text）。
func lastTextValue(item *jsonwalk.Value) *jsonwalk.Value {
	// 形态 1：content 是字符串
	if c := item.Member("content"); c != nil {
		if s, err := c.String(); err == nil && !strings.HasPrefix(s, "[") {
			return c
		}
		// 形态 2：content 是数组，元素有 text 字段
		var last *jsonwalk.Value
		for i := 0; i < c.Len(); i++ {
			el := c.Index(i)
			if el == nil {
				continue
			}
			if t := el.Member("text"); t != nil {
				last = t
			}
		}
		if last != nil {
			return last
		}
	}
	// 形态 3：直接 text 字段
	return item.Member("text")
}
