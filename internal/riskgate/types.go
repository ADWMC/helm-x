// Package riskgate 是请求前置风险闸门：判定一次请求在"上游网络安全过滤"(A 层) 与
// "模型自身合规判断"(B 层) 上的风险，并按判定定向改写用户请求。
//
// 设计依据见 docs/RISKGATE-DESIGN.md。三条硬约束：
//  1. 判定单位是**载荷**而非用户一句话 —— 我们的注入提示词本身可能是触发器；
//  2. 判定分两轴（filter / compliance），因为对应的改法完全不同；
//  3. 判定失败一律直通，绝不阻断请求。
package riskgate

// Risk 是三档风险等级。
type Risk string

const (
	RiskNone Risk = "none"
	RiskLow  Risk = "low"
	RiskHigh Risk = "high"
)

// Locus 指出风险来自哪一层文本。
type Locus string

const (
	LocusNone      Locus = "none"
	LocusUser      Locus = "user"
	LocusInjection Locus = "injection"
	LocusBoth      Locus = "both"
)

// Payload 是判定输入：三层文本。
type Payload struct {
	// UserText 是最新一条用户消息。
	UserText string
	// InjectionText 是我们注入的提示词全文（为空表示本次请求不注入）。
	InjectionText string
	// HistoryTail 是会话历史尾部摘要（可选，用于累积风险）。
	HistoryTail string
}

// Trigger 是一条被命中的触发器及其位置。
type Trigger struct {
	Text  string `json:"text"`
	Where Locus  `json:"where"`
	Kind  string `json:"kind"` // attack / evasion / product / target
}

// Verdict 是闸门的判定结果，字段名与前端 frontend/src/api/backend.ts 的 GateInfo 对齐。
type Verdict struct {
	Locus           Locus     `json:"locus"`
	FilterRisk      Risk      `json:"filterRisk"`
	ComplianceRisk  Risk      `json:"complianceRisk"`
	Triggers        []Trigger `json:"triggers"`
	ContractGap     []string  `json:"contractGap"`
	Anchor          string    `json:"anchor"`
	Rewrite         string    `json:"rewrite,omitempty"`
	EntityRetention float64   `json:"entityRetention"`
	Why             string    `json:"why"`
	// Source 记录判定来源：l0 / model / cache，便于统计模型命中率。
	Source string `json:"source"`
}

// Empty 表示不需要处理的判定。
func (v Verdict) Empty() bool {
	return v.FilterRisk == "" && v.ComplianceRisk == ""
}

// Config 是闸门运行配置。与 %APPDATA%\helmx.config.json 的 riskgate 段一一对应。
type Config struct {
	Enabled bool `json:"enabled"`
	// Mode 是 shadow（只记录不改写）或 enforce（按判定改写）。
	Mode string `json:"mode"`
	// Profile 决定判定方式：substr-gateway 走本地精确匹配，cyber-openai/generic 走模型。
	Profile string `json:"profile"`
	// HotModel 是热路径判定模型（延迟优先）。
	HotModel string `json:"hot_model"`
	// LabelModel 是离线标注模型（质量优先，不在用户路径上）。
	LabelModel  string `json:"label_model"`
	BaseURL     string `json:"base_url"`
	APIKey      string `json:"api_key"`
	TimeoutSec  int    `json:"timeout_sec"`
	CacheTTLSec int    `json:"cache_ttl_sec"`
	// EnforceRatio 是灰度比例（0-100）。
	EnforceRatio int `json:"enforce_ratio"`
	// Budget 是每会话上限，超限自动降级为 shadow。
	Budget Budget `json:"budget"`
}

// Budget 是每会话预算。
type Budget struct {
	PerSessionJudge   int `json:"per_session_judge"`
	PerSessionRewrite int `json:"per_session_rewrite"`
	PerRequestTokens  int `json:"per_request_tokens"`
}

// DefaultConfig 返回保守默认值：关闭、影子模式、本地快筛优先。
func DefaultConfig() Config {
	return Config{
		Enabled:      false,
		Mode:         "shadow",
		Profile:      "cyber-openai",
		HotModel:     "nvidia/nemotron-3.5-bolt-e4b",
		LabelModel:   "meta/muse-glimmer-30b",
		BaseURL:      "https://integrate.api.nvidia.com/v1",
		TimeoutSec:   8,
		CacheTTLSec:  900,
		EnforceRatio: 5,
		Budget: Budget{
			PerSessionJudge:   3,
			PerSessionRewrite: 2,
			PerRequestTokens:  4000,
		},
	}
}
