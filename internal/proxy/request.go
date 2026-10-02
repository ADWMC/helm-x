package proxy

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/ADWMC/helm-x/internal/protocol/jsonwalk"
)

// RequestView 是对上游请求体的只读投影。
//
// 【按实测设计】真实 codex 0.159.3 的请求形态见 docs/FINDINGS-phase1.md：
//   - 顶层有 model / stream / input / tools / reasoning / store / ...
//   - **没有** max_output_tokens，**没有** instructions
//   - input[0] 可能是 type=additional_tools（非消息，无 content）
//
// 因此所有可选字段都用指针，缺失是常态而非异常。
type RequestView struct {
	// Path 是入站请求路径（如 /v1/responses）。
	Path string

	// Body 是原始字节，永不重新序列化（INV-4）。
	Body []byte

	Model  string
	Stream bool

	// root 是 Body 的解析结果；nil 表示无法解析（→ 原样透传）。
	root *jsonwalk.Value

	// input 是 input 数组节点。
	input *jsonwalk.Value

	Tools     *jsonwalk.Value
	Reasoning *jsonwalk.Value
	Store     *jsonwalk.Value
}

// ParseRequest 解析入站请求体。
//
// 解析失败返回 error —— 调用方必须**原样透传**，不得猜测（INV-9）。
func ParseRequest(path string, body []byte) (*RequestView, error) {
	root, err := jsonwalk.Parse(body)
	if err != nil {
		return nil, err
	}
	v := &RequestView{Path: path, Body: body, root: root}

	if m := root.Member("model"); m != nil {
		v.Model = m.MustString()
	}
	if s := root.Member("stream"); s != nil && s.Kind == jsonwalk.KindBool {
		if b, err := s.Bool(); err == nil {
			v.Stream = b
		}
	}
	v.input = root.Member("input")
	v.Tools = root.Member("tools")
	v.Reasoning = root.Member("reasoning")
	v.Store = root.Member("store")
	return v, nil
}

// InputCount 返回 input 数组的条目数。
func (v *RequestView) InputCount() int {
	if v == nil || v.input == nil {
		return 0
	}
	return v.input.Len()
}

// LastUserMessage 返回最后一条真实用户消息。
//
// 与旧版 extract_user_message（proxy.cpp:291-333）语义一致：
// 跳过含 <environment_context> 的条目 —— 那是 codex 注入的环境信息，不是用户输入。
func (v *RequestView) LastUserMessage() (string, bool) {
	if v == nil || v.input == nil {
		return "", false
	}
	last := ""
	found := false
	for i := 0; i < v.input.Len(); i++ {
		item := v.input.Index(i)
		if item == nil {
			continue
		}
		if role := item.Member("role"); role == nil || role.MustString() != "user" {
			continue
		}
		for _, t := range textsOfMessage(item) {
			if t == "" || strings.Contains(t, "<environment_context>") {
				continue
			}
			last = t
			found = true
		}
	}
	return last, found
}

// ConversationText 返回精简的对话上下文，供改写器理解用户在做什么。
//
// 只取最近的若干条，避免把整个 45KB 请求喂给改写模型。
func (v *RequestView) ConversationText() string {
	if v == nil || v.input == nil {
		return ""
	}
	const maxTurns = 6
	var parts []string
	for i := 0; i < v.input.Len(); i++ {
		item := v.input.Index(i)
		if item == nil {
			continue
		}
		role := ""
		if r := item.Member("role"); r != nil {
			role = r.MustString()
		}
		if role != "user" && role != "assistant" {
			continue
		}
		for _, t := range textsOfMessage(item) {
			t = strings.TrimSpace(t)
			if t == "" || strings.Contains(t, "<environment_context>") {
				continue
			}
			parts = append(parts, role+": "+truncateRunes(t, 400))
		}
	}
	if len(parts) > maxTurns {
		parts = parts[len(parts)-maxTurns:]
	}
	return strings.Join(parts, "\n")
}

// textsOfMessage 取出一个 input 条目里全部 input_text / output_text 的正文。
func textsOfMessage(item *jsonwalk.Value) []string {
	content := item.Member("content")
	if content == nil {
		return nil
	}
	var out []string
	// content 既可能是数组，也可能是字符串（chat 风格）
	if content.Kind == jsonwalk.KindString {
		return []string{content.MustString()}
	}
	for i := 0; i < content.Len(); i++ {
		c := content.Index(i)
		if c == nil {
			continue
		}
		if t := c.Member("text"); t != nil {
			out = append(out, t.MustString())
		}
	}
	return out
}

// InjectSystem 把系统指令作为**第一条** input 条目插入。
//
// 【已实测验证】docs/FINDINGS-phase1.md §3：
// 该位置被真实上游接受，且指令确实生效（模型回复中出现了指定标记词）。
// 这与旧版做法一致（proxy.cpp:539-542），无需改动。
//
// 已存在同前缀的注入时幂等，避免每次重发都叠加。
func (v *RequestView) InjectSystem(instruction string) ([]byte, bool, error) {
	if v == nil || instruction == "" {
		return nil, false, nil
	}
	if v.input == nil {
		// 没有 input 数组：结构不认识，不猜（INV-9）
		return v.Body, false, nil
	}

	// 幂等检查：第一条已是 message/system 且含相同开头，则不重复注入
	if first := v.input.Index(0); first != nil {
		if role := first.Member("role"); role != nil && role.MustString() == "system" {
			for _, t := range textsOfMessage(first) {
				if strings.HasPrefix(t, injectPrefix(instruction)) {
					return v.Body, true, nil
				}
			}
		}
	}

	msg := map[string]any{
		"type": "message",
		"role": "system",
		"content": []any{
			map[string]any{"type": "input_text", "text": instruction},
		},
	}
	enc, err := json.Marshal(msg)
	if err != nil {
		return nil, false, err
	}

	out, err := jsonwalk.InsertFirst(v.Body, v.input, enc)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func injectPrefix(instruction string) string {
	const n = 64
	if len(instruction) <= n {
		return instruction
	}
	return instruction[:n]
}

// BuildCleanSession 重建一个精简请求：只留必要的顶层字段 + 一条用户消息。
//
// 对应旧版 build_clean_session（proxy.cpp:441-509），但按**实测的请求形态**调整：
// 旧版会去找 max_output_tokens（实测不存在），新版只在字段确实存在时保留。
//
// 保留：model / reasoning / tools（若存在）
// 丢弃：全部历史、store、include、prompt_cache_key 等会话相关字段
func (v *RequestView) BuildCleanSession(userMsg string) ([]byte, error) {
	if v == nil {
		return nil, errNilView
	}

	var buf bytes.Buffer
	buf.WriteByte('{')

	model := v.Model
	if model == "" {
		model = "gpt-5.6-terra" // 与旧版默认值一致
	}
	writeField(&buf, "model", model, true)

	// input：单条用户消息
	buf.WriteString(`,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":`)
	enc, err := json.Marshal(userMsg)
	if err != nil {
		return nil, err
	}
	buf.Write(enc)
	buf.WriteString(`}]}]`)

	// 保留 reasoning（影响模型行为，丢了会改变输出风格）
	if v.Reasoning != nil {
		buf.WriteString(`,"reasoning":`)
		buf.Write(v.Reasoning.Raw)
	}
	// 保留 tools（丢了模型无法调用工具）
	if v.Tools != nil && v.Tools.Kind == jsonwalk.KindArray && v.Tools.Len() > 0 {
		buf.WriteString(`,"tools":`)
		buf.Write(v.Tools.Raw)
	}

	// 与旧版一致：clean session 一律非流式，便于完整判定
	buf.WriteString(`,"stream":false`)
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func writeField(buf *bytes.Buffer, key, val string, first bool) {
	if !first {
		buf.WriteByte(',')
	}
	enc, _ := json.Marshal(val)
	buf.WriteString(`"` + key + `":`)
	buf.Write(enc)
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
