package riskgate

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

// Gate 是闸门主体。零值不可用，必须用 New 构造。
type Gate struct {
	cfg   Config
	cache *cache

	mu       sync.Mutex
	sessions map[string]*sessionStat

	// now 便于测试注入。
	judgeFn func(ctx context.Context, p Payload) (Verdict, error)
}

type sessionStat struct {
	judged   int
	rewrites int
}

// ErrDisabled 表示闸门未启用（调用方直通）。
var ErrDisabled = errors.New("riskgate: disabled")

// New 构造闸门。
func New(cfg Config) *Gate {
	g := &Gate{cfg: cfg, cache: newCache(0), sessions: make(map[string]*sessionStat)}
	if cfg.CacheTTLSec > 0 {
		g.cache = newCache(secondsToDuration(cfg.CacheTTLSec))
	}
	g.judgeFn = g.judgeWithModel
	return g
}

// Config 返回当前配置（供前端展示）。
func (g *Gate) Config() Config { return g.cfg }

// Judge 判定一次请求。第二个返回值 ok=false 表示"不影响请求"（未启用/失败/预算超限），
// 调用方必须直通 —— 闸门永远不阻断请求。
func (g *Gate) Judge(ctx context.Context, session string, p Payload) (Verdict, bool) {
	if g == nil || !g.cfg.Enabled {
		return Verdict{}, false
	}
	if strings.TrimSpace(p.UserText) == "" {
		return Verdict{}, false
	}
	if !g.takeBudget(session) {
		return Verdict{Why: "预算超限，已降级为直通", Source: "budget"}, false
	}

	// ① 本地快筛：零成本，且对子串型中转本身就是准确判定。
	l0 := Scan(p.UserText + "\n" + p.InjectionText)
	injL0 := Scan(p.InjectionText)
	if len(injL0.Triggers) > 0 {
		l0.Triggers = append(l0.Triggers, markWhere(injL0.Triggers, LocusInjection)...)
	}

	// ② 干净 → 直接返回，不调模型（这是热路径的主要路径）
	if l0.Clean && g.cfg.Profile == "substr-gateway" {
		v := Verdict{
			Locus: LocusNone, FilterRisk: RiskNone, ComplianceRisk: RiskNone,
			Why: "本地快筛无信号", Source: "l0",
		}
		return v, true
	}

	// ③ 缓存
	key := cacheKey(g.cfg.Profile, p)
	if v, ok := g.cache.get(key); ok {
		v.Source = "cache"
		return v, true
	}

	// ④ 模型判定；失败则回退 L0 结论（仍然返回，只是来源标注）
	v, err := g.judgeFn(ctx, p)
	if err != nil {
		fallback := Verdict{
			Locus:          locusOf(l0.Triggers),
			FilterRisk:     l0.FilterRisk,
			ComplianceRisk: l0.ComplianceRisk,
			Triggers:       l0.Triggers,
			ContractGap:    l0.ContractGap,
			Why:            "模型判定失败，回退本地快筛",
			Source:         "l0-fallback",
		}
		return fallback, true
	}
	v.Source = "model"
	if v.Locus == "" {
		v.Locus = locusOf(l0.Triggers)
	}
	// 合并：模型可能漏报，本地信号取并集（宁多勿少，因为只影响改写与否）
	v.Triggers = mergeTriggers(v.Triggers, l0.Triggers)
	if rank(v.FilterRisk) < rank(l0.FilterRisk) {
		v.FilterRisk = l0.FilterRisk
	}
	if rank(v.ComplianceRisk) < rank(l0.ComplianceRisk) {
		v.ComplianceRisk = l0.ComplianceRisk
	}
	g.cache.put(key, v)
	return v, true
}

// judgeWithModel 调模型拿判定。
func (g *Gate) judgeWithModel(ctx context.Context, p Payload) (Verdict, error) {
	model := g.cfg.HotModel
	if model == "" {
		return Verdict{}, errors.New("riskgate: 未配置判定模型")
	}
	raw, err := newClient(g.cfg, model).complete(ctx, p)
	if err != nil {
		return Verdict{}, err
	}
	return parseVerdict(raw)
}

// parseVerdict 解析模型输出。只取第一个完整 JSON 对象，容忍前后缀噪声。
func parseVerdict(raw string) (Verdict, error) {
	start := strings.Index(raw, "{")
	if start < 0 {
		return Verdict{}, errors.New("riskgate: 判定不含 JSON")
	}
	depth, inStr, esc := 0, false, false
	end := -1
	for i := start; i < len(raw); i++ {
		c := raw[i]
		switch {
		case esc:
			esc = false
		case c == '\\' && inStr:
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				end = i + 1
			}
		}
		if end > 0 {
			break
		}
	}
	if end < 0 {
		return Verdict{}, errors.New("riskgate: JSON 不完整")
	}

	var wire struct {
		FilterRisk     string   `json:"filter_risk"`
		ComplianceRisk string   `json:"compliance_risk"`
		Triggers       []string `json:"triggers"`
		ContractGap    []string `json:"contract_gap"`
		Why            string   `json:"why"`
		Rewrite        string   `json:"rewrite"`
	}
	if err := json.Unmarshal([]byte(raw[start:end]), &wire); err != nil {
		return Verdict{}, err
	}
	v := Verdict{
		FilterRisk:     normalizeRisk(wire.FilterRisk),
		ComplianceRisk: normalizeRisk(wire.ComplianceRisk),
		ContractGap:    wire.ContractGap,
		Why:            wire.Why,
		Rewrite:        wire.Rewrite,
	}
	for _, t := range wire.Triggers {
		if strings.TrimSpace(t) != "" {
			v.Triggers = append(v.Triggers, Trigger{Text: t, Where: LocusUser, Kind: "model"})
		}
	}
	return v, nil
}

// 工具函数

func normalizeRisk(s string) Risk {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "high":
		return RiskHigh
	case "low":
		return RiskLow
	case "none":
		return RiskNone
	}
	return RiskNone
}

func rank(r Risk) int {
	switch r {
	case RiskHigh:
		return 2
	case RiskLow:
		return 1
	}
	return 0
}

func locusOf(ts []Trigger) Locus {
	hasUser, hasInj := false, false
	for _, t := range ts {
		switch t.Where {
		case LocusInjection:
			hasInj = true
		case LocusUser:
			hasUser = true
		}
	}
	switch {
	case hasUser && hasInj:
		return LocusBoth
	case hasInj:
		return LocusInjection
	case hasUser:
		return LocusUser
	}
	return LocusNone
}

func markWhere(ts []Trigger, l Locus) []Trigger {
	out := make([]Trigger, 0, len(ts))
	for _, t := range ts {
		t.Where = l
		out = append(out, t)
	}
	return out
}

func mergeTriggers(a, b []Trigger) []Trigger {
	seen := map[string]bool{}
	out := make([]Trigger, 0, len(a)+len(b))
	for _, t := range append(append([]Trigger{}, a...), b...) {
		key := strings.ToLower(t.Text) + "|" + string(t.Where)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	return out
}

// takeBudget 扣一次会话预算。超限返回 false。
func (g *Gate) takeBudget(session string) bool {
	limit := g.cfg.Budget.PerSessionJudge
	if limit <= 0 {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	st, ok := g.sessions[session]
	if !ok {
		st = &sessionStat{}
		g.sessions[session] = st
	}
	if st.judged >= limit {
		return false
	}
	st.judged++
	return true
}

func secondsToDuration(s int) time.Duration { return time.Duration(s) * time.Second }

// NewForTest 构造一个用桩判定函数替代模型的闸门，供跨包测试使用。
// 语义与 New 一致：仍然走 L0 快筛、缓存与预算，只是不访问网络。
func NewForTest(cfg Config, judge func(ctx context.Context, p Payload) (Verdict, error)) *Gate {
	g := New(cfg)
	if judge != nil {
		g.judgeFn = judge
	}
	return g
}
