// Package assets 提供内嵌资源访问。
//
// 职责：把编译期嵌入的提示词/规则/QA 暴露给其他包。
//
// 【Anti-Glance XOR 混淆机制】
// 静态提示词和安全规则包含红队与攻防术语。若以纯文本明文嵌入二进制，
// 会导致 Windows Defender / 火绒等杀软的启发式静态扫描误报（如 Backdoor/CobaltStrike.bh）。
// 本包资源在编译期通过 tools/genassets 执行 XOR 混淆并固化至 data_gen.go，
// 运行时通过 Get() 透明解混淆并带读写锁缓存，杜绝只读数据段 (.rdata) 明文字串泄漏。
//
//go:generate go run ../../tools/genassets
package assets

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// 资源逻辑名 → 嵌入路径。
const (
	pathPromptCTF      = "data/prompt-ctf-scoring.md"
	pathPromptV2       = "data/prompt-v2.md"
	pathPromptV21      = "data/prompt-v2.1.md"
	pathTamperRules    = "data/tamper_rules.txt"
	pathRewritePrompt  = "data/rewrite_prompt.txt"
	pathQA             = "data/qa.json"
)

var (
	mu    sync.RWMutex
	cache = map[string]string{}
)

// Get 按嵌入逻辑路径读取资源。数据在编译期经 XOR 混淆存储，运行时透明解码并带读写锁缓存。
func Get(path string) string {
	mu.RLock()
	if v, ok := cache[path]; ok {
		mu.RUnlock()
		return v
	}
	mu.RUnlock()

	raw, ok := obfuscatedData[path]
	if !ok {
		return ""
	}

	decoded := make([]byte, len(raw))
	for i, b := range raw {
		decoded[i] = b ^ xorKey[i%len(xorKey)]
	}
	s := string(decoded)

	mu.Lock()
	cache[path] = s
	mu.Unlock()
	return s
}

// PromptMode 是一个提示词模式。
type PromptMode struct {
	// ID 是配置里存的标识（与旧版 helmx.config.json 的 prompt_mode 兼容）。
	ID string `json:"id"`
	// Name 是界面显示名。
	Name string `json:"name"`
	// Description 是一句话说明。
	Description string `json:"description"`
	// Bytes 是提示词大小，供界面展示。
	Bytes int `json:"bytes"`
	// Default 标记默认模式。
	Default bool `json:"default"`
}

// promptModes 定义全部可用模式。
//
// ID 与旧版保持一致（"default" / "v45" / "deepseek"），
// 因为用户已有的 helmx.config.json 里存的就是这些值。
var promptModes = []struct {
	mode PromptMode
	path string
}{
	{PromptMode{ID: "v2.1", Name: "v2.1 证据闭环版", Description: "三级证据评级 + 规范占位符 + 回滚闭环", Default: true}, pathPromptV21},
	{PromptMode{ID: "v2", Name: "v2 完成态契约", Description: "补缺口 + VERIFIED 工件 + 人设加厚"}, pathPromptV2},
	{PromptMode{ID: "ctf", Name: "CTF 计分制", Description: "经典 2.5KB，CTF 计分制 + 输出锁"}, pathPromptCTF},
}

// Prompt 按模式 ID 返回提示词正文。未知 ID 回退到默认模式 (v2.1)。
func Prompt(modeID string) string {
	for _, m := range promptModes {
		if m.mode.ID == modeID {
			if s := Get(m.path); s != "" {
				return s
			}
		}
	}
	return Get(pathPromptV21)
}

// PromptModes 列出全部模式，附实际字节数。只返回内容非空的模式。
func PromptModes() []PromptMode {
	out := make([]PromptMode, 0, len(promptModes))
	for _, m := range promptModes {
		s := Get(m.path)
		if s == "" {
			continue
		}
		pm := m.mode
		pm.Bytes = len(s)
		out = append(out, pm)
	}
	return out
}

// IsValidPromptMode 报告模式 ID 是否可用。
func IsValidPromptMode(id string) bool {
	for _, m := range promptModes {
		if m.mode.ID == id && Get(m.path) != "" {
			return true
		}
	}
	return false
}

// TamperRules 返回拒绝句式规则文本。
func TamperRules() string { return Get(pathTamperRules) }

// RewritePrompt 返回改写器的系统提示词。
func RewritePrompt() string { return Get(pathRewritePrompt) }

// QAItem 是一条常见问题。
type QAItem struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// QAData 是 QA 数据文件的结构。
type QAData struct {
	Version   int      `json:"version"`
	UpdatedAt string   `json:"updated_at"`
	Items     []QAItem `json:"items"`
}

// QA 返回内嵌的 QA 数据（离线回退用）。
func QA() QAData {
	var d QAData
	raw := Get(pathQA)
	if raw == "" {
		return d
	}
	_ = json.Unmarshal([]byte(raw), &d)
	return d
}

// List 列出全部嵌入资源的逻辑名，供自检与诊断。
func List() []string {
	out := make([]string, 0, len(obfuscatedData))
	for p := range obfuscatedData {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Summary 返回资源清单的可读摘要，供自检输出。
func Summary() string {
	var sb strings.Builder
	for _, p := range List() {
		fmt.Fprintf(&sb, "  %-40s %6d B\n", p, len(Get(p)))
	}
	return sb.String()
}
