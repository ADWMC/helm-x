package svc

import (
	"context"
	"fmt"
	"time"

	"github.com/ADWMC/helm-x/internal/config"
	"github.com/ADWMC/helm-x/internal/riskgate"
)

// RiskGateService 暴露风险闸门的配置、探测与离线回放。
//
// 与 RewriterService 同构：凭据只给前缀提示，绝不明文回传（INV-10）。
type RiskGateService struct {
	s *Services
}

// RiskGateView 是脱敏后的闸门配置 + 运行统计。
type RiskGateView struct {
	Enabled      bool       `json:"enabled"`
	Mode         string     `json:"mode"`
	Profile      string     `json:"profile"`
	HotModel     string     `json:"hotModel"`
	LabelModel   string     `json:"labelModel"`
	BaseURL      string     `json:"baseUrl"`
	KeyHint      string     `json:"keyHint"`
	HasKey       bool       `json:"hasKey"`
	TimeoutSec   int        `json:"timeoutSec"`
	CacheTTLSec  int        `json:"cacheTtlSec"`
	EnforceRatio int        `json:"enforceRatio"`
	Budget       BudgetView `json:"budget"`
}

// BudgetView 是每会话预算。
type BudgetView struct {
	PerSessionJudge   int `json:"perSessionJudge"`
	PerSessionRewrite int `json:"perSessionRewrite"`
	PerRequestTokens  int `json:"perRequestTokens"`
}

// Get 返回当前配置（凭据脱敏）。
func (r *RiskGateService) Get() (RiskGateView, error) {
	s := r.s.rt.Store.Get()
	g := s.RiskGate
	return RiskGateView{
		Enabled:      g.Enabled,
		Mode:         g.Mode,
		Profile:      g.Profile,
		HotModel:     g.HotModel,
		LabelModel:   g.LabelModel,
		BaseURL:      g.BaseURL,
		KeyHint:      maskKey(g.APIKey),
		HasKey:       g.APIKey != "",
		TimeoutSec:   g.TimeoutSec,
		CacheTTLSec:  g.CacheTTLSec,
		EnforceRatio: g.EnforceRatio,
		Budget: BudgetView{
			PerSessionJudge:   g.BudgetPerSessionJudge,
			PerSessionRewrite: g.BudgetPerSessionRewrite,
			PerRequestTokens:  g.BudgetPerRequestTokens,
		},
	}, nil
}

// RiskGateInput 是保存请求。APIKey 为空串表示"保持原值"。
type RiskGateInput struct {
	Enabled      bool       `json:"enabled"`
	Mode         string     `json:"mode"`
	Profile      string     `json:"profile"`
	HotModel     string     `json:"hotModel"`
	LabelModel   string     `json:"labelModel"`
	BaseURL      string     `json:"baseUrl"`
	APIKey       string     `json:"apiKey"`
	TimeoutSec   int        `json:"timeoutSec"`
	CacheTTLSec  int        `json:"cacheTtlSec"`
	EnforceRatio int        `json:"enforceRatio"`
	Budget       BudgetView `json:"budget"`
}

// Set 保存配置。校验比改写器更严：mode/profile 是行为分叉点，写错会静默失效。
func (r *RiskGateService) Set(in RiskGateInput) error {
	if in.Enabled {
		if in.BaseURL == "" {
			return fmt.Errorf("启用风险闸门需要填写 API 地址")
		}
		if in.HotModel == "" {
			return fmt.Errorf("启用风险闸门需要填写热路径判定模型")
		}
	}
	switch in.Mode {
	case "", "shadow", "enforce":
	default:
		return fmt.Errorf("mode 只能是 shadow 或 enforce")
	}
	switch in.Profile {
	case "", "substr-gateway", "cyber-openai", "generic":
	default:
		return fmt.Errorf("profile 只能是 substr-gateway / cyber-openai / generic")
	}
	if in.EnforceRatio < 0 || in.EnforceRatio > 100 {
		return fmt.Errorf("enforceRatio 必须在 0-100")
	}

	err := r.s.rt.Store.Update(func(s *config.Settings) {
		s.RiskGate.Enabled = in.Enabled
		s.RiskGate.Mode = in.Mode
		s.RiskGate.Profile = in.Profile
		s.RiskGate.HotModel = in.HotModel
		s.RiskGate.LabelModel = in.LabelModel
		s.RiskGate.BaseURL = in.BaseURL
		s.RiskGate.TimeoutSec = in.TimeoutSec
		s.RiskGate.CacheTTLSec = in.CacheTTLSec
		s.RiskGate.EnforceRatio = in.EnforceRatio
		s.RiskGate.BudgetPerSessionJudge = in.Budget.PerSessionJudge
		s.RiskGate.BudgetPerSessionRewrite = in.Budget.PerSessionRewrite
		s.RiskGate.BudgetPerRequestTokens = in.Budget.PerRequestTokens
		if in.APIKey != "" {
			s.RiskGate.APIKey = in.APIKey
		}
	})
	if err != nil {
		return err
	}
	r.s.rt.Logger.Info("风险闸门配置已保存（启用=%v，模式=%s，模型=%s）", in.Enabled, in.Mode, in.HotModel)
	r.s.emit(EventConfigChg, nil)
	return nil
}

// ProbeResult 是一次单条判定预览的结果。**不发上游请求**，只跑闸门。
type ProbeResult struct {
	OK       bool             `json:"ok"`
	Verdict  riskgate.Verdict `json:"verdict"`
	Duration int64            `json:"durationMs"`
	Error    string           `json:"error,omitempty"`
}

// Probe 对一段文本做判定预览（不发送任何对话请求）。
//
// 用途：界面里贴一句请求，直接看闸门会怎么判 —— 这是校准与调试的唯一入口。
func (r *RiskGateService) Probe(text string) (ProbeResult, error) {
	start := time.Now()
	if text == "" {
		return ProbeResult{OK: false, Error: "请输入一段要判定的请求"}, nil
	}
	g := r.s.rt.Gate()
	if g == nil {
		return ProbeResult{OK: false, Error: "风险闸门未启用或配置不完整"}, nil
	}
	v, ok := g.Judge(context.Background(), "probe", riskgate.Payload{UserText: text})
	if !ok {
		return ProbeResult{OK: false, Error: "判定未生效（未启用/预算/失败）"}, nil
	}
	return ProbeResult{OK: true, Verdict: v, Duration: time.Since(start).Milliseconds()}, nil
}
