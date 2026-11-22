// deploy.go 是提示词的**部署语义**：同一段文本放到哪个位置、覆盖什么。
//
// 为什么单独成文件（2026-11-22）：注入位置是独立于内容的杠杆。现有实现只往
// input[] 插一条 system 消息（request.go:InjectSystemAt），而实测过的竞赛臂
// 要求**整段替换顶层 instructions**（官方 base_instructions 21.3KB 被换掉）。
// 两者不是"同一个东西换个位置"，文本、评测结论、失败模式都不同，因此显式建模
// 为三档，而不是塞进一个 bool。
//
// 三档语义（与 helm-a `settings.Deploy` 对齐）：
//
//	insert        往 input[] 插 system 消息（现状，默认）
//	append        追加到顶层 instructions 末尾（零额外暴露位）
//	replace-base  整段替换顶层 instructions（竞赛臂；官方 base 被覆盖）
//
// 不变量：
//   - 一律走 jsonwalk 字节级手术，不重新序列化（INV-4）；未涉及的字段一个字节不动。
//   - 顶层没有 instructions 时用 InsertFirst 造一个成员；结构不认识则不猜（INV-9）。
package proxy

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ADWMC/helm-x/internal/protocol/jsonwalk"
)

// DeployMode 是提示词部署语义。
type DeployMode string

const (
	// DeployInsert 是历史行为：input[] 第一条插 system 消息。
	DeployInsert DeployMode = "insert"
	// DeployAppend 把指令追加到顶层 instructions 末尾（保留官方 base）。
	DeployAppend DeployMode = "append"
	// DeployReplaceBase 整段替换顶层 instructions（覆盖官方 base_instructions）。
	DeployReplaceBase DeployMode = "replace-base"
)

// ParseDeployMode 归一化部署语义。空串与未知值都落到 insert
// （未知值不静默改写用户配置 —— 归一化发生在 config.normalize，见那里的白名单）。
func ParseDeployMode(s string) DeployMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "append", "append-instructions", "append_instructions", "tail":
		return DeployAppend
	case "replace-base", "replace_base", "replacebase", "replace":
		return DeployReplaceBase
	default:
		return DeployInsert
	}
}

// Valid 报告该语义是否在白名单内（供配置校验）。
func (m DeployMode) Valid() bool {
	switch m {
	case DeployInsert, DeployAppend, DeployReplaceBase:
		return true
	}
	return false
}

// Instructions 返回顶层 instructions 的当前文本。
//
// 真实 codex 0.159.3 的请求里没有这个字段（docs/FINDINGS-phase1.md），
// 0.167 起出现；两种形态都要能处理，所以返回 (文本, 是否存在)。
func (v *RequestView) Instructions() (string, bool) {
	if v == nil || v.root == nil {
		return "", false
	}
	m := v.root.Member("instructions")
	if m == nil || m.Kind != jsonwalk.KindString {
		return "", false
	}
	return m.MustString(), true
}

// InjectInstructions 按部署语义把指令写进顶层 instructions。
//
// 返回值 (新请求体, 是否真的改写了, error)。第三个返回值只表示"结构不支持"，
// 调用方应回落到原样透传（INV-9）而不是猜。
func (v *RequestView) InjectInstructions(instruction string, mode DeployMode) ([]byte, bool, error) {
	if v == nil || instruction == "" {
		return nil, false, nil
	}
	if v.root == nil || v.root.Kind != jsonwalk.KindObject {
		return v.Body, false, fmt.Errorf("%w: 请求体顶层不是对象", errNilView)
	}

	cur, has := v.Instructions()

	switch mode {
	case DeployReplaceBase:
		// 幂等：已经是这段文本就不动（重复请求体不会膨胀）
		if has && cur == instruction {
			return v.Body, true, nil
		}
		if has {
			out, err := jsonwalk.ReplaceString(v.Body, v.root.Member("instructions"), instruction)
			return out, true, err
		}
		return v.insertInstructionsMember(instruction)

	case DeployAppend:
		if has {
			// 幂等：以该指令结尾（允许空白差异）则不重复追加
			if strings.HasSuffix(strings.TrimSpace(cur), strings.TrimSpace(instruction)) {
				return v.Body, true, nil
			}
			sep := "\n\n"
			if cur == "" || strings.HasSuffix(cur, "\n") {
				sep = ""
			}
			out, err := jsonwalk.ReplaceString(v.Body, v.root.Member("instructions"), cur+sep+instruction)
			return out, true, err
		}
		return v.insertInstructionsMember(instruction)
	}

	// DeployInsert 由 InjectSystemAt 处理，这里不重复实现
	return v.Body, false, nil
}

// insertInstructionsMember 在顶层对象里造一个 instructions 成员。
// 位置：紧跟在 '{' 之后（InsertFirst 语义），其余成员字节不动。
func (v *RequestView) insertInstructionsMember(instruction string) ([]byte, bool, error) {
	enc, err := json.Marshal(instruction)
	if err != nil {
		return nil, false, err
	}
	member := make([]byte, 0, len(enc)+20)
	member = append(member, `"instructions":`...)
	member = append(member, enc...)
	out, err := jsonwalk.InsertFirst(v.Body, v.root, member)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}
