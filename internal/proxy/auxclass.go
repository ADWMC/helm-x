package proxy

import (
	"encoding/json"
	"strings"
)

// IsAuxiliaryRequest 判断一条请求是不是 codex 自己发的**辅助请求**（非主请求）。
//
// 为什么必须区分：codex 除了用户对话，还会发标题生成、压缩、摘要、工具建议等请求。
// 对这些请求做改写或判定既浪费额度，也可能破坏它们的功能（例如压缩请求被改写得跑不通）。
//
// 四级判定，顺序即优先级（同 GptBypass 的 classify_user_request 思路）：
//  1. Header 显式：x-helmx-gate=on|off
//  2. Metadata 显式：{metadata:{helmx_gate:true|false}}
//  3. 强辅助模式：命中标题/摘要/压缩类措辞
//  4. 低信号短句：≤120 字、无换行、无 URL，且带辅助提示词特征
//
// 默认返回 false（视为主请求），因为漏判的代价是"多判一次"，误判的代价是"该判的没判"。
func IsAuxiliaryRequest(headers map[string][]string, body []byte) bool {
	if v := headerValue(headers, "x-helmx-gate"); v != "" {
		return strings.EqualFold(v, "off") || v == "0"
	}

	var parsed struct {
		Metadata map[string]any `json:"metadata"`
		Input    []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"input"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil {
		if parsed.Metadata != nil {
			if raw, ok := parsed.Metadata["helmx_gate"]; ok {
				if b, ok := raw.(bool); ok {
					return !b
				}
			}
			if _, ok := parsed.Metadata["skip_optimize"]; ok {
				return true
			}
		}
	}

	last := lastUserTextFromBody(parsed.Input)
	if last == "" {
		return false
	}

	// 强辅助模式：这些请求不是用户在问事
	for _, pat := range strongAuxPatterns {
		if strings.Contains(strings.ToLower(last), pat) {
			return true
		}
	}

	// 低信号短句 + 辅助词
	if len([]rune(last)) <= 120 && !strings.Contains(last, "\n") && !strings.Contains(last, "http") {
		for _, pat := range weakAuxPatterns {
			if strings.Contains(strings.ToLower(last), pat) {
				return true
			}
		}
	}
	return false
}

var strongAuxPatterns = []string{
	"generate a title", "suggest a title", "conversation title",
	"summarize the conversation", "compaction summary", "summarise this",
	"生成标题", "总结这段对话", "压缩上下文", "生成摘要",
}

var weakAuxPatterns = []string{
	"title", "summar", "compact", "abstract", "标签", "标题", "摘要",
}

// headerValue 大小写不敏感地取头。
func headerValue(headers map[string][]string, key string) string {
	for k, vs := range headers {
		if strings.EqualFold(k, key) && len(vs) > 0 {
			return vs[0]
		}
	}
	return ""
}

// lastUserTextFromBody 从已解析的 input 里取最后一条 user 文本（跳过环境上下文）。
func lastUserTextFromBody(input []struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}) string {
	for i := len(input) - 1; i >= 0; i-- {
		it := input[i]
		if it.Role != "user" {
			continue
		}
		switch c := it.Content.(type) {
		case string:
			if !strings.Contains(c, "<environment_context>") {
				return c
			}
		case []any:
			for j := len(c) - 1; j >= 0; j-- {
				if m, ok := c[j].(map[string]any); ok {
					if t, ok := m["text"].(string); ok && !strings.Contains(t, "<environment_context>") {
						return t
					}
				}
			}
		}
	}
	return ""
}
