// transform.go 是请求层改写通道的**报文级手术**：
//
//	encode    把最后一条 user 消息的 text parts 整体 mojibake 化，还原说明进 instructions
//	codebook  抽片段换伪词，映射段进 instructions
//
// 算法在 internal/inject（纯文本），这里只负责"在 JSON 的哪个位置动手"。
// 全部走 jsonwalk 定点替换：未涉及的字段一字节不动（INV-4）；解析失败原样透传（INV-9）。
//
// 两条通道互斥，encode 优先（比 codebook 强：整段不可读，不只换几个词）。
package proxy

import (
	"fmt"

	"github.com/ADWMC/helm-x/internal/inject"
	"github.com/ADWMC/helm-x/internal/protocol/jsonwalk"
)

// TextPart 是一个可改写的文本片段：明文 + 它在请求体里的值节点。
//
// 带节点而不是带字节偏移：偏移在每次手术后会失效，而节点每次都是从**当前**
// 请求体重新解析出来的，天然对应最新字节。
type TextPart struct {
	Text string
	val  *jsonwalk.Value
}

// LastUserTextParts 返回**最后一条**非 assistant/system item 的全部 text part。
//
// 与 LastUserMessage 同一条 item（各 part 明文按序拼接即其内容），
// 区别是保留结构：改写要逐个 part 替换，不能把它们合并再塞回去。
func (v *RequestView) LastUserTextParts() []TextPart {
	if v == nil || v.input == nil {
		return nil
	}
	for i := v.input.Len() - 1; i >= 0; i-- {
		item := v.input.Index(i)
		if item == nil {
			continue
		}
		if role := item.Member("role"); role != nil {
			r := role.MustString()
			if r == "assistant" || r == "system" {
				continue
			}
		}
		content := item.Member("content")
		if content == nil || content.Kind != jsonwalk.KindArray {
			continue
		}
		var parts []TextPart
		for j := 0; j < content.Len(); j++ {
			c := content.Index(j)
			if c == nil {
				continue
			}
			txt := c.Member("text")
			if txt == nil || txt.Kind != jsonwalk.KindString {
				continue
			}
			parts = append(parts, TextPart{Text: txt.MustString(), val: txt})
		}
		if len(parts) > 0 {
			return parts
		}
	}
	return nil
}

// SpliceTextPart 用新明文整值替换该 part（自动转义），返回新请求体。
//
// 区间必须来自**同一份**请求体的 LastUserTextParts —— 换过一轮就重新解析。
func (v *RequestView) SpliceTextPart(p TextPart, newText string) ([]byte, error) {
	if v == nil || p.val == nil {
		return nil, errNilView
	}
	return jsonwalk.ReplaceString(v.Body, p.val, newText)
}

// applyTransforms 按配置对出站报文做请求层改写，返回新报文与被应用的变换标签。
//
// 顺序与互斥：encode 优先；两者都关时原样返回。任何一步失败都保留已完成的
// 部分并记 note，不整体回滚（与 helm-a 的 A3 一致：宁可半改，不要丢报文）。
func (e *Engine) applyTransforms(body []byte, rec *RequestRecord) []byte {
	if e.opts.Config == nil {
		return body
	}
	if e.opts.Config.EncodeEnabled() {
		out, n, note := applyEncode(body)
		if n > 0 {
			rec.Transforms = append(rec.Transforms, fmt.Sprintf("encode:%d", n))
		}
		if note != "" {
			rec.Note = joinNonEmpty(rec.Note, note)
		}
		return out
	}
	if slots := e.opts.Config.CodebookSlots(); slots > 0 {
		out, n, note := applyCodebook(body, slots)
		if n > 0 {
			rec.Transforms = append(rec.Transforms, fmt.Sprintf("codebook:%d", n))
		}
		if note != "" {
			rec.Note = joinNonEmpty(rec.Note, note)
		}
		return out
	}
	return body
}

// applyEncode 把最后一条 user 消息的全部 text part 做 mojibake，并追加还原说明。
func applyEncode(body []byte) (out []byte, partsChanged int, note string) {
	v, err := ParseRequest("", body)
	if err != nil {
		return body, 0, "encode 跳过：请求体不可解析"
	}
	parts := v.LastUserTextParts()
	if len(parts) == 0 {
		return body, 0, ""
	}

	cur := body
	changed := 0
	for i := range parts {
		v2, perr := ParseRequest("", cur)
		if perr != nil {
			return cur, changed, "encode 中断：中间态不可解析"
		}
		ps := v2.LastUserTextParts()
		if i >= len(ps) {
			break
		}
		nb, serr := v2.SpliceTextPart(ps[i], inject.Mojibake(ps[i].Text))
		if serr != nil {
			return cur, changed, "encode 中断：" + serr.Error()
		}
		cur = nb
		changed++
	}
	if changed == 0 {
		return cur, 0, ""
	}

	v3, perr := ParseRequest("", cur)
	if perr != nil {
		return cur, changed, "encode 说明未写入：中间态不可解析"
	}
	nb, ok, aerr := v3.InjectInstructions(inject.EncodeNote, DeployAppend)
	if aerr != nil || !ok {
		return cur, changed, "encode 说明未写入"
	}
	return nb, changed, ""
}

// applyCodebook 抽片段换伪词，映射段追加进 instructions。
func applyCodebook(body []byte, slots int) (out []byte, spans int, note string) {
	v, err := ParseRequest("", body)
	if err != nil {
		return body, 0, "codebook 跳过：请求体不可解析"
	}
	parts := v.LastUserTextParts()
	if len(parts) == 0 {
		return body, 0, ""
	}
	total := 0
	for _, p := range parts {
		total += len([]rune(p.Text))
	}
	if total < 8 {
		return body, 0, "" // user 明文过短不启用（边界）
	}

	cur := body
	var all []inject.Span
	for i := 0; i < len(parts) && len(all) < slots; i++ {
		v2, perr := ParseRequest("", cur)
		if perr != nil {
			return cur, len(all), "codebook 中断：中间态不可解析"
		}
		ps := v2.LastUserTextParts()
		if i >= len(ps) {
			break
		}
		newText, sp := inject.Apply(ps[i].Text, slots-len(all))
		if len(sp) == 0 {
			continue
		}
		nb, serr := v2.SpliceTextPart(ps[i], newText)
		if serr != nil {
			return cur, len(all), "codebook 中断：" + serr.Error()
		}
		cur = nb
		all = append(all, sp...)
	}
	if len(all) == 0 {
		return cur, 0, ""
	}

	v3, perr := ParseRequest("", cur)
	if perr != nil {
		return cur, len(all), "codebook 映射段未写入：中间态不可解析"
	}
	nb, ok, aerr := v3.InjectInstructions(inject.MappingSegment(all), DeployAppend)
	if aerr != nil || !ok {
		return cur, len(all), "codebook 映射段未写入"
	}
	return nb, len(all), ""
}

// joinNonEmpty 拼接备注（分号分隔，跳过空串）。
func joinNonEmpty(dst, add string) string {
	switch {
	case add == "":
		return dst
	case dst == "":
		return add
	}
	return dst + "; " + add
}
