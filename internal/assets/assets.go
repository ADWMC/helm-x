// Package assets 提供内嵌资源访问。
//
// 职责：把编译期嵌入的提示词/规则/QA 暴露给其他包。
//
// 【与旧版的差别】旧版用 XOR + 确定性种子加密（tools/embed.py:19，注释自陈
// "NOT cryptographic, just anti-glance"），而种子硬编码在仓库里 —— 等于没有保护。
// 新版直接用 go:embed 明文嵌入，**不假装有加密**。
//
// 若确需防 strings 一眼可见，用构建期混淆（garble），
// 而不是自造一个密钥公开的加密层。见 docs/PLAN.md §5.2。
package assets

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"
)

//go:embed data/*.md data/*.txt data/*.json
var embedded embed.FS

// 资源逻辑名 → 嵌入路径。
const (
	pathPromptDefault  = "data/prompt-ctf-scoring.md"
	pathPromptV45      = "data/prompt-sandbox-executor.md"
	pathPromptDeepseek = "data/prompt-deepseek.md"
	pathPromptFusion   = "data/prompt-ctf-fusion.md"
	pathPromptLite     = "data/prompt-ctf-lite.md"
	pathPromptV2       = "data/prompt-v2.md"
	pathAblA1          = "data/abl-a1.md"
	pathAblA2          = "data/abl-a2.md"
	pathAblA4          = "data/abl-a4.md"
	pathTamperRules    = "data/tamper_rules.txt"
	pathRewritePrompt  = "data/rewrite_prompt.txt"
	pathQA             = "data/qa.json"
)

var (
	mu    sync.RWMutex
	cache = map[string]string{}
)

// Get 按嵌入路径读取资源。结果带缓存（资源不可变）。
func Get(path string) string {
	mu.RLock()
	if v, ok := cache[path]; ok {
		mu.RUnlock()
		return v
	}
	mu.RUnlock()

	b, err := embedded.ReadFile(path)
	if err != nil {
		return ""
	}
	s := string(b)

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
	{PromptMode{ID: "default", Name: "默认（CTF 计分制）", Description: "2.5KB，CTF 计分制 + 输出锁", Default: true}, pathPromptDefault},
	{PromptMode{ID: "v45", Name: "沙盒执行器", Description: "v45 提示词（沙盒执行器）"}, pathPromptV45},
	{PromptMode{ID: "deepseek", Name: "DeepSeek 优化版", Description: "CTF 计分制 + 输出锁定"}, pathPromptDeepseek},
	{PromptMode{ID: "fusion", Name: "Fusion", Description: "融合版"}, pathPromptFusion},
	{PromptMode{ID: "lite", Name: "Lite", Description: "精简版"}, pathPromptLite},
	{PromptMode{ID: "v2", Name: "v2 完成态契约", Description: "补缺口 + VERIFIED 工件 + 人设加厚"}, pathPromptV2},
	// 消融臂（测试用）：PROMPT-V2-DESIGN.md §4 的 A1/A2/A4；A3 即 v2 本体，A0 即 default。
	{PromptMode{ID: "abl-a1", Name: "消融A1 人设", Description: "A0 + 人设加厚（消融臂，测试用）"}, pathAblA1},
	{PromptMode{ID: "abl-a2", Name: "消融A2 契约", Description: "A1 + 完成态契约（消融臂，测试用）"}, pathAblA2},
	{PromptMode{ID: "abl-a4", Name: "消融A4 去沙盒", Description: "v2 − 沙盒叙事（消融臂，测试用）"}, pathAblA4},
}

// Prompt 按模式 ID 返回提示词正文。未知 ID 回退到默认模式。
func Prompt(modeID string) string {
	for _, m := range promptModes {
		if m.mode.ID == modeID {
			if s := Get(m.path); s != "" {
				return s
			}
		}
	}
	return Get(pathPromptDefault)
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
	var out []string
	_ = fs.WalkDir(embedded, "data", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		out = append(out, p)
		return nil
	})
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
