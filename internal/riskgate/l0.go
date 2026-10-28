package riskgate

import (
	"regexp"
	"strings"
)

// L0 是本地快筛：不调模型，靠"信号组合"给出第一档判定。
//
// 为什么需要它（而不是每题都问模型）：
//   - 模型热路径 1.1–2.0s，而绝大多数请求（写代码、看类型、改配置）零风险；
//   - 上游若是 new-api 类中转，其过滤是纯子串匹配（strings.ToLower + Aho-Corasick），
//     本地命中即 100% 准确，根本不需要模型。
//
// 权重来自实测：26 次 KEYGEN-1 尝试 + 5 次被 cyber 拦截的样本（PT-1/PT-2/KDRV-1 等）。
// 样本量有限，权重必须用回放集重新拟合（docs/RISKGATE-DESIGN.md §2.1）。

// signalKind 是信号类别，权重见 weightOf。
type signalKind string

const (
	kindAttack  signalKind = "attack"  // 攻击性动词
	kindTarget  signalKind = "target"  // 真实目标标识
	kindEvasion signalKind = "evasion" // 规避语义
	kindProduct signalKind = "product" // 具名风控产品
)

func weightOf(k signalKind) int {
	switch k {
	case kindEvasion, kindProduct:
		return 2
	case kindAttack, kindTarget:
		return 1
	}
	return 0
}

// signal 是一条本地信号（子串或正则）。
type signal struct {
	kind    signalKind
	pattern string
	regex   *regexp.Regexp
}

func (s signal) match(text string) bool {
	if s.regex != nil {
		return s.regex.MatchString(text)
	}
	return strings.Contains(text, s.pattern)
}

func re(p string) *regexp.Regexp { return regexp.MustCompile(p) }

// signals 是本地信号表。刻意保持"短而硬"——只收实测出现过的形态，
// 避免变成一张永远不全的词海（这正是被否掉的 input_normalizer 的失败原因）。
var signals = []signal{
	// ── 规避语义（权重 2）──
	{kind: kindEvasion, pattern: "绕过"},
	{kind: kindEvasion, pattern: "规避"},
	{kind: kindEvasion, pattern: "免杀"},
	{kind: kindEvasion, pattern: "过检"},
	{kind: kindEvasion, pattern: "过检测"},
	{kind: kindEvasion, pattern: "过杀软"},
	{kind: kindEvasion, pattern: "隐藏进程"},
	{kind: kindEvasion, pattern: "不被发现"},
	{kind: kindEvasion, pattern: "看不到"},
	{kind: kindEvasion, pattern: "绕过校验"},
	{kind: kindEvasion, pattern: "bypass"},
	{kind: kindEvasion, pattern: "evasion"},
	{kind: kindEvasion, pattern: "hide process"},
	// ── 具名风控产品（权重 2）──
	{kind: kindProduct, pattern: "ACE", regex: re(`(?i)\bACE\b|ace-base`)},
	{kind: kindProduct, pattern: "EAC"},
	{kind: kindProduct, pattern: "BattlEye"},
	{kind: kindProduct, pattern: "hCaptcha"},
	{kind: kindProduct, pattern: "reCAPTCHA"},
	{kind: kindProduct, pattern: "Turnstile"},
	{kind: kindProduct, pattern: "人机验证"},
	{kind: kindProduct, pattern: "验证码"},
	// ── 攻击性动词（权重 1）──
	{kind: kindAttack, pattern: "拿下"},
	{kind: kindAttack, pattern: "提权"},
	{kind: kindAttack, pattern: "注入"},
	{kind: kindAttack, pattern: "脱壳"},
	{kind: kindAttack, pattern: "抓包"},
	{kind: kindAttack, pattern: "批量注册"},
	{kind: kindAttack, pattern: "注册机"},
	{kind: kindAttack, pattern: "横向"},
	{kind: kindAttack, pattern: "域控"},
	{kind: kindAttack, pattern: "DCSync"},
	{kind: kindAttack, pattern: "webshell"},
	{kind: kindAttack, pattern: "撞库"},
	{kind: kindAttack, pattern: "免接口"},
	// ── 真实目标标识（权重 1）──
	{kind: kindTarget, pattern: "域", regex: re(`(?i)\bDC0\d\b|域控|域管|corp\.local`)},
	{kind: kindTarget, pattern: "内网", regex: re(`内网|内网横向`)},
	{kind: kindTarget, pattern: "具体 IP", regex: re(`\b\d{1,3}(\.\d{1,3}){3}\b`)},
	{kind: kindTarget, pattern: "CVE", regex: re(`(?i)CVE-\d{4}-\d+`)},
	{kind: kindTarget, pattern: "Steam", regex: re(`(?i)steam`)},
}

// L0Result 是本地快筛的结论。
type L0Result struct {
	FilterRisk     Risk
	ComplianceRisk Risk
	Triggers       []Trigger
	ContractGap    []string
	// Score 是加权和，用于解释与校准。
	Score int
	// Clean 表示零信号：可以直接直通，不调模型。
	Clean bool
}

// complianceSignals 是"可执行性"打分用的信号（与 filter 无关）。
var (
	// 产物形态：要代码/脚本/补丁/表格/步骤
	reArtefact = re(`(?i)代码|脚本|补丁|表格|驱动|工具|生成器|函数|命令|配置|payload|exploit|poc|分镜|场景|段落`)
	// 第一步动作：查询/检查/运行
	reFirstStep = re(`(?i)检查|查看|跑一下|运行|试试|看下|查询|探测|分析|试一下|先看`)
	// 验证方式
	reVerify = re(`(?i)验证|测试|确认|对比|跑通|通过|复现|断言`)
	// 技术上不存在的动作：纯本地过服务端校验
	reImpossible = re(`纯本地.{0,8}(过|通过|绕过).{0,8}(验证|校验|captcha|人机)|离线.{0,6}过.{0,6}验证`)
)

// Scan 对一段文本做本地快筛。
func Scan(text string) L0Result {
	var res L0Result
	if strings.TrimSpace(text) == "" {
		res.Clean = true
		return res
	}

	seen := map[string]bool{}
	for _, sig := range signals {
		if !sig.match(text) {
			continue
		}
		// 同一 pattern 只计一次
		if seen[sig.pattern] {
			continue
		}
		seen[sig.pattern] = true
		res.Score += weightOf(sig.kind)
		res.Triggers = append(res.Triggers, Trigger{Text: sig.pattern, Where: LocusUser, Kind: string(sig.kind)})
	}

	switch {
	case res.Score >= 3:
		res.FilterRisk = RiskHigh
	case res.Score >= 1:
		res.FilterRisk = RiskLow
	default:
		res.FilterRisk = RiskNone
	}

	// 可执行性打分（docs/RISKGATE-DESIGN.md §2.2）
	cs := 0
	if reArtefact.MatchString(text) {
		cs++
	}
	if reFirstStep.MatchString(text) {
		cs++
	}
	if reVerify.MatchString(text) {
		cs++
	}
	if reImpossible.MatchString(text) {
		cs -= 2
		res.ContractGap = append(res.ContractGap, "要求技术上不存在的动作")
	} else {
		cs++
	}
	if len(res.ContractGap) == 0 && cs < 3 {
		res.ContractGap = append(res.ContractGap, "缺少验证方式或参数规则")
	}

	switch {
	case cs >= 2:
		res.ComplianceRisk = RiskNone
	case cs == 1:
		res.ComplianceRisk = RiskLow
	default:
		res.ComplianceRisk = RiskHigh
	}

	res.Clean = res.FilterRisk == RiskNone && res.ComplianceRisk == RiskNone
	return res
}
