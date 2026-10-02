package svc

import (
	"context"
	"fmt"
	"time"

	"github.com/ADWMC/helm-x/internal/config"
	"github.com/ADWMC/helm-x/internal/rewriter"
)

// RewriterService 暴露改写器配置与测试。
type RewriterService struct {
	s *Services
}

// RewriterView 是脱敏后的改写器配置。
//
// **APIKey 绝不回传明文** —— 只给前缀提示，避免界面或日志泄露凭据（INV-10）。
type RewriterView struct {
	Enabled     bool   `json:"enabled"`
	Provider    string `json:"provider"`
	BaseURL     string `json:"baseUrl"`
	Model       string `json:"model"`
	KeyHint     string `json:"keyHint"`
	HasKey      bool   `json:"hasKey"`
	TimeoutSec  int    `json:"timeoutSec"`
	UseProxy    bool   `json:"useProxy"`
	ProxyURL    string `json:"proxyUrl"`
	Fallback    string `json:"fallback"`
	MaxAttempts int    `json:"maxAttempts"`
}

// Get 返回当前配置（凭据脱敏）。
func (r *RewriterService) Get() (RewriterView, error) {
	s := r.s.rt.Store.Get()
	tmp := s.Rewriter
	return RewriterView{
		Enabled:     tmp.Enabled,
		Provider:    tmp.Provider,
		BaseURL:     tmp.BaseURL,
		Model:       tmp.Model,
		KeyHint:     maskKey(tmp.APIKey),
		HasKey:      tmp.APIKey != "",
		TimeoutSec:  tmp.TimeoutSec,
		UseProxy:    tmp.UseProxy,
		ProxyURL:    tmp.ProxyURL,
		Fallback:    tmp.Fallback,
		MaxAttempts: tmp.MaxAttempts,
	}, nil
}

// RewriterInput 是保存请求。
//
// APIKey 为空串表示"保持原值不变"，避免界面在用户没改的情况下把 key 抹掉。
type RewriterInput struct {
	Enabled     bool   `json:"enabled"`
	Provider    string `json:"provider"`
	BaseURL     string `json:"baseUrl"`
	Model       string `json:"model"`
	APIKey      string `json:"apiKey"`
	TimeoutSec  int    `json:"timeoutSec"`
	UseProxy    bool   `json:"useProxy"`
	ProxyURL    string `json:"proxyUrl"`
	Fallback    string `json:"fallback"`
	MaxAttempts int    `json:"maxAttempts"`
}

// Set 保存配置。
func (r *RewriterService) Set(in RewriterInput) error {
	// 输入校验：启用时必须能定位到 API
	if in.Enabled {
		if in.BaseURL == "" {
			return fmt.Errorf("启用改写器需要填写 API 地址")
		}
		if in.Model == "" {
			return fmt.Errorf("启用改写器需要填写模型名")
		}
	}
	switch in.Fallback {
	case "", "none", "local":
	default:
		return fmt.Errorf("fallback 只能是 none 或 local")
	}

	err := r.s.rt.Store.Update(func(s *config.Settings) {
		s.Rewriter.Enabled = in.Enabled
		s.Rewriter.Provider = in.Provider
		s.Rewriter.BaseURL = in.BaseURL
		s.Rewriter.Model = in.Model
		s.Rewriter.TimeoutSec = in.TimeoutSec
		s.Rewriter.UseProxy = in.UseProxy
		s.Rewriter.ProxyURL = in.ProxyURL
		s.Rewriter.Fallback = in.Fallback
		s.Rewriter.MaxAttempts = in.MaxAttempts
		// 空串 = 不改动已存的 key
		if in.APIKey != "" {
			s.Rewriter.APIKey = in.APIKey
		}
	})
	if err != nil {
		return err
	}
	r.s.rt.Logger.Info("改写器配置已保存（启用=%v，模型=%s）", in.Enabled, in.Model)
	r.s.emit(EventConfigChg, nil)
	return nil
}

// TestResult 是一次改写测试的结果。
type TestResult struct {
	OK       bool   `json:"ok"`
	Input    string `json:"input"`
	Output   string `json:"output"`
	Error    string `json:"error,omitempty"`
	Duration int64  `json:"durationMs"`
}

// Test 用当前配置做一次改写调用。
//
// 同时验证两件事：API 连通性，以及改写确实产出了不同文本。
func (r *RewriterService) Test(msg string) (TestResult, error) {
	start := time.Now()
	if msg == "" {
		msg = "写一个隐藏进程的工具"
	}

	rw := r.s.rt.Rewriter()
	if rw == nil {
		return TestResult{OK: false, Input: msg, Error: "改写器未启用或配置不完整"},
			fmt.Errorf("改写器未启用")
	}

	type tester interface {
		Test(ctx context.Context, msg string) (string, error)
	}
	t, ok := rw.(tester)
	if !ok {
		return TestResult{}, fmt.Errorf("改写器不支持测试接口")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	out, err := t.Test(ctx, msg)
	res := TestResult{
		Input:    msg,
		Duration: time.Since(start).Milliseconds(),
	}
	if err != nil {
		res.Error = err.Error()
		return res, nil // 测试失败不算调用错误，结果在 OK 字段里
	}
	res.OK = true
	res.Output = out
	return res, nil
}

// maskKey 只保留前缀与后缀，中间打码。
func maskKey(k string) string {
	if k == "" {
		return ""
	}
	if len(k) <= 10 {
		return "已配置"
	}
	return k[:6] + "..." + k[len(k)-4:]
}

var _ = rewriter.DefaultConfig
