// Package assets 提供内嵌资源访问。
//
// 职责：把编译期嵌入的提示词/规则/QA 暴露给其他包。
//
// 【Anti-Glance 加密机制】
// 静态提示词和安全规则包含红队与攻防术语。若以纯文本明文嵌入二进制，
// 会导致 Windows Defender / 火绒等杀软的启发式静态扫描误报（如 Backdoor/CobaltStrike.bh）。
// 本包资源在编译期由 tools/genassets 用 AES-256-GCM 加密并固化至 data_gen.go，
// 运行时解密并缓存，避免只读数据段（.rdata）出现明文字串。
//
// 密钥：K = SHA-256(HELMX_ASSET_KEY)，未设置时用内置开发种子。
// 二进制里只留 SHA-256(K) 摘要，不落密钥本身；构建与运行密钥不一致时
// 所有资源读出为空，并通过 HealthCheck() 给出明确原因（不静默降级）。
//
//go:generate go run ../../tools/genassets
package assets

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
)

// devKeySeed 与 tools/genassets 保持一致：未设置 HELMX_ASSET_KEY 时的开发种子。
const devKeySeed = "helm-x-dev-asset-key-do-not-ship"

// 资源逻辑名 → 嵌入路径。
const (
	pathPromptCTF     = "data/prompt-ctf-scoring.md"
	pathPromptV2      = "data/prompt-v2.md"
	pathPromptV21     = "data/prompt-v2.1.md"
	pathPromptV4      = "data/prompt-v4.md"
	pathPromptV5      = "data/prompt-v5.md"
	pathPromptV6      = "data/prompt-v6.md"
	pathPromptMDX61   = "data/prompt-mdx61.md"
	pathPromptSeagull = "data/prompt-seagull.md"
	pathPromptXiaoliu = "data/prompt-xiaoliu.md"
	pathPromptXw      = "data/prompt-xwsandbox.md"
	pathPromptChang   = "data/prompt-chang.md"
	pathTamperRules   = "data/tamper_rules.txt"
	pathRewritePrompt = "data/rewrite_prompt.txt"
	pathQA            = "data/qa.json"
)

var (
	mu    sync.RWMutex
	cache = map[string]string{}

	keyOnce  sync.Once
	keyCache []byte
	keyErr   error
)

// resolveKey 计算并校验运行期密钥。摘要不符说明构建与运行用了不同的种子。
func resolveKey() ([]byte, error) {
	keyOnce.Do(func() {
		seed := os.Getenv("HELMX_ASSET_KEY")
		if seed == "" {
			seed = devKeySeed
		}
		k := sha256.Sum256([]byte(seed))
		d := sha256.Sum256(k[:])
		if len(assetKeyDigest) != len(d) || !bytes.Equal(d[:], assetKeyDigest) {
			if assetKeyFromEnv {
				keyErr = fmt.Errorf(
					"assets: 资产密钥不匹配 —— 构建时用了 HELMX_ASSET_KEY，当前环境未设置或值不同；" +
						"请用同一个 HELMX_ASSET_KEY 启动，或清空该变量后重新构建")
			} else {
				keyErr = fmt.Errorf(
					"assets: 资产密钥不匹配 —— 构建时用的是内置开发种子，但当前环境设置了 HELMX_ASSET_KEY；" +
						"请清空该变量重新运行")
			}
			return
		}
		keyCache = append([]byte(nil), k[:]...)
	})
	return keyCache, keyErr
}

// decode 解密 blob（nonce || GCM 密文）。name 是资源逻辑路径，作为 AAD。
func decode(name string, blob []byte) (string, error) {
	key, err := resolveKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("assets: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("assets: %w", err)
	}
	ns := gcm.NonceSize()
	if len(blob) < ns {
		return "", fmt.Errorf("assets: %s 数据长度不足", name)
	}
	plain, err := gcm.Open(nil, blob[:ns], blob[ns:], []byte(name))
	if err != nil {
		return "", fmt.Errorf("assets: %s 解密失败: %w", name, err)
	}
	return string(plain), nil
}

// Get 按嵌入逻辑路径读取资源（透明解密 + 读写锁缓存）。
// 解密失败返回空串；需要原因时调 HealthCheck。
func Get(path string) string {
	mu.RLock()
	if v, ok := cache[path]; ok {
		mu.RUnlock()
		return v
	}
	mu.RUnlock()

	blob, ok := obfuscatedData[path]
	if !ok {
		return ""
	}
	s, err := decode(path, blob)
	if err != nil {
		return ""
	}

	mu.Lock()
	cache[path] = s
	mu.Unlock()
	return s
}

// HealthCheck 报告内嵌资源是否可解密，供自检给出可执行的失败原因。
func HealthCheck() error {
	if len(obfuscatedData) == 0 {
		return errors.New("assets: 未嵌入任何资源（data_gen.go 为空）")
	}
	for p := range obfuscatedData {
		if s := Get(p); s == "" {
			if _, err := resolveKey(); err != nil {
				return err
			}
			return fmt.Errorf("assets: %s 解密结果为空", p)
		}
	}
	return nil
}

// KeySource 返回构建期密钥来源，供自检展示。
func KeySource() string {
	if assetKeyFromEnv {
		return "HELMX_ASSET_KEY"
	}
	return "内置开发种子"
}

// PromptMode 是一个提示词模式。
type PromptMode struct {
	// ID 是配置里存的标识。统一 `ctf` 前缀 + 阶梯版本：ctf / ctf-v2 / ctf-v2.1。
	ID string `json:"id"`
	// Name 是界面显示名。
	Name string `json:"name"`
	// Description 是一句话说明。
	Description string `json:"description"`
	// Models 是该模式实测适配的模型代数（界面直接显示，避免选错）。
	Models string `json:"models"`
	// Bytes 是提示词大小，供界面展示。
	Bytes int `json:"bytes"`
	// Default 标记默认模式。
	Default bool `json:"default"`
}

// promptModes 定义全部可用模式。
//
// 【命名约定（2026-10）】全部带 ctf 前缀，后缀是提示词阶梯版本；适配的模型
// 代数写在 Models 里，**不做进 ID** —— ID 一旦带模型名，模型换代就要改配置。
//
//	ctf      2.5KB 经典计分制    → gpt-5.6 系
//	ctf-v2   完成态契约          → gpt-6.0 系
//	ctf-v2.1 证据闭环（默认）    → gpt-6.1 系
//	ctf-v4   厚人设 + 单点拒绝面  → 待测
var promptModes = []struct {
	mode PromptMode
	path string
}{
	{PromptMode{ID: "ctf", Name: "CTF 经典计分制", Description: "2.5KB · 计分淘汰 + 输出锁 + 首轮归一化", Models: "gpt-5.6 系"}, pathPromptCTF},
	{PromptMode{ID: "ctf-v2", Name: "CTF v2 完成态契约", Description: "补缺口 + VERIFIED 工件 + 人设加厚", Models: "gpt-6.0 系"}, pathPromptV2},
	{PromptMode{ID: "ctf-v2.1", Name: "CTF v2.1 证据闭环", Description: "三级证据评级 + 规范占位符 + 回滚闭环", Models: "gpt-6.1 系", Default: true}, pathPromptV21},
	{PromptMode{ID: "ctf-v4", Name: "CTF v4 厚人设 + 拒绝面单点", Description: "厚人设 + 类目映射 + 三段式交付契约 + 87 条禁词", Models: "gpt-6.x（待测）"}, pathPromptV4},
	{PromptMode{ID: "ctf-v5", Name: "CTF v5 事务版", Description: "英文正文 + 28 行类目路由 + 犹豫句翻译 + 四件套事务验收", Models: "gpt-6.x / 6.1（待测）"}, pathPromptV5},
	{PromptMode{ID: "ctf-v6", Name: "CTF v6 融合版", Description: "v2.1 沙盒+双账本计分 × v5 的 PROCESS/四件套/占位符，7.6KB 零暴露面", Models: "gpt-6.1 系（待测）"}, pathPromptV6},
	{PromptMode{ID: "ctf-mdx", Name: "MDX-Tom v6.1-rc2（外部，原样）", Description: "对照用：别人的事务状态机提示词，仅用于横向对比", Models: "gpt-6.1 系（待测）"}, pathPromptMDX61},
	{PromptMode{ID: "ctf-seagull", Name: "海鸥 3.0（外部，原样）", Description: "对照用：别人家的沙盒+技能路由提示词，仅用于横向对比", Models: "gpt-6.1 系（待测）"}, pathPromptSeagull},
	{PromptMode{ID: "ctf-xiaoliu", Name: "xiaoliu-pojia ctf-sandbox（外部，原样）", Description: "对照用（仅横向对比）", Models: "gpt-6.1 系（待测）"}, pathPromptXiaoliu},
	{PromptMode{ID: "ctf-xw", Name: "xw9114 沙箱协议 v2（外部，原样）", Description: "对照用（仅横向对比）", Models: "gpt-6.1 系（待测）"}, pathPromptXw},
	{PromptMode{ID: "ctf-chang", Name: "chAng redteam Jailbreak gpt-5.6（外部，原样）", Description: "对照用（仅横向对比）", Models: "gpt-6.1 系（待测）"}, pathPromptChang},
}

// promptAliases 是历史 ID → 现行 ID 的映射。
//
// 必须保留：用户 %APPDATA%\helmx.config.json 里存的是旧 ID（本机就是 "v2"），
// 旧版还曾把内置提示词存成 "default"。直接按新 ID 校验会让升级用户
// 看到"未知的提示词模式"。
var promptAliases = map[string]string{
	"default": "ctf", // 旧版内置默认（当时是 2.5KB CTF 版）
	"v2":      "ctf-v2",
	"v2.1":    "ctf-v2.1",
	"v4":      "ctf-v4",
	"v5":      "ctf-v5",
	"v6":      "ctf-v6",
	"mdx":     "ctf-mdx",
	"seagull": "ctf-seagull",
	"xiaoliu": "ctf-xiaoliu",
	"xw":      "ctf-xw",
	"chang":   "ctf-chang",
}

// CanonicalPromptMode 把历史 ID 归一化为现行 ID；未知 ID 原样返回。
func CanonicalPromptMode(id string) string {
	if v, ok := promptAliases[id]; ok {
		return v
	}
	return id
}

// Prompt 按模式 ID 返回提示词正文。历史 ID 自动归一化；未知 ID 回退默认 (ctf-v2.1)。
func Prompt(modeID string) string {
	modeID = CanonicalPromptMode(modeID)
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

// IsValidPromptMode 报告模式 ID 是否可用（历史 ID 同样接受）。
func IsValidPromptMode(id string) bool {
	id = CanonicalPromptMode(id)
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
