// Package runtime 把各领域包组装成可运行的服务。
//
// 职责：持有配置与单例组件（日志、TAMPER 引擎、代理引擎、观测层），
// 供 CLI 与 GUI 共用。**不含业务逻辑** —— 只做装配与生命周期。
//
// 存在的理由：CLI（helmx proxy）与 GUI（Wails 服务层）需要同一套组件，
// 但都不应该知道彼此的装配细节。这个包是它们共同的装配点。
package runtime

import (
	"context"
	"path/filepath"
	"sync"

	"github.com/ADWMC/helm-x/internal/assets"
	"github.com/ADWMC/helm-x/internal/codexcfg"
	"github.com/ADWMC/helm-x/internal/config"
	"github.com/ADWMC/helm-x/internal/logging"
	"github.com/ADWMC/helm-x/internal/obs"
	"github.com/ADWMC/helm-x/internal/proxy"
	"github.com/ADWMC/helm-x/internal/tamper"
)

// Runtime 是装配好的运行时。
type Runtime struct {
	Store  *config.Store
	Logger *logging.Logger

	mu      sync.Mutex
	home    codexcfg.Home
	hasHome bool
	engine  *proxy.Engine
	tamper  *tamper.Engine
	// capture / recorder：观测层（报文捕获 + 请求记录落盘）
	capture  *obs.Capture
	recorder *obs.Recorder
	// sink 接收代理事件（日志 + 请求记录）
	sink proxy.EventSink
}

// New 构造运行时。
func New(store *config.Store, logger *logging.Logger) *Runtime {
	rt := &Runtime{Store: store, Logger: logger}
	if h, err := codexcfg.Find(); err == nil {
		rt.home, rt.hasHome = h, true
	}
	rt.tamper = buildTamper()
	// 启动自愈：上次被强杀时把 codex 从这个没人监听的本地端口救回来（见 heal.go）
	if rt.hasHome {
		healStaleProxy(rt.home, func(level, format string, args ...any) {
			if level == "warn" {
				rt.Logger.Warn(format, args...)
				return
			}
			rt.Logger.Info(format, args...)
		})
	}

	// 观测目录统一放 %APPDATA%\helmx\{capture,records}：不与配置文件同级
	dataDir := filepath.Join(filepath.Dir(store.Path()), "helmx")
	s := store.Get()
	rt.capture = obs.NewCapture(filepath.Join(dataDir, "capture"), s.CaptureEnabled)
	rt.capture.SetRedact(s.CaptureRedact)
	rt.recorder = obs.NewRecorder(filepath.Join(dataDir, "records"), 7)
	return rt
}

// Home 返回 codex 配置目录。
func (r *Runtime) Home() (codexcfg.Home, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.home, r.hasHome
}

// SetHome 允许调用方覆盖 codex home（GUI 里用户可改）。
func (r *Runtime) SetHome(h codexcfg.Home) {
	r.mu.Lock()
	r.home, r.hasHome = h, true
	r.mu.Unlock()
}

func buildTamper() *tamper.Engine {
	e, errs := tamper.New(assets.TamperRules())
	if len(errs) > 0 {
		// 规则文件里有个别非法正则不应让整个引擎失效
		_ = errs
	}
	return e
}

// Tamper 返回规则引擎。
func (r *Runtime) Tamper() *tamper.Engine { return r.tamper }

// ProxyConfig 实现 proxy.ConfigProvider。
//
// 这是**热生效**的落点（INV-5）：每次请求都读一次 Store，
// 而 Store.Refresh 用 mtime 判断是否需要重新解析文件。
// 旧版每请求重新解析 JSON（proxy.cpp:884），新版只在文件变化时解析。
type ProxyConfig struct {
	rt *Runtime
}

// NewProxyConfig 构造配置提供者。
func (r *Runtime) NewProxyConfig() *ProxyConfig { return &ProxyConfig{rt: r} }

// PromptInstruction 返回当前要注入的指令。
func (c *ProxyConfig) PromptInstruction() string {
	c.rt.Store.Refresh()
	s := c.rt.Store.Get()
	return assets.Prompt(s.PromptMode)
}

// InjectEvery 返回注入频率：每 N 次请求注入 1 次（按会话计数）。
func (c *ProxyConfig) InjectEvery() int {
	c.rt.Store.Refresh()
	s := c.rt.Store.Get()
	if s.InjectEvery <= 0 {
		return 1
	}
	return s.InjectEvery
}

// FallbackUserAgent 返回 UA 兜底值（空 = 不补）。
func (c *ProxyConfig) FallbackUserAgent() string {
	c.rt.Store.Refresh()
	return c.rt.Store.Get().ForwardUserAgent
}

// Deploy 返回提示词部署语义（insert / append / replace-base）。
func (c *ProxyConfig) Deploy() string {
	c.rt.Store.Refresh()
	return c.rt.Store.Get().Deploy
}

// EncodeEnabled 报告是否启用 user 全文编码。
func (c *ProxyConfig) EncodeEnabled() bool {
	c.rt.Store.Refresh()
	return c.rt.Store.Get().EncodeEnabled
}

// CodebookSlots 返回代号表槽位数；0 = 未启用。
func (c *ProxyConfig) CodebookSlots() int {
	c.rt.Store.Refresh()
	s := c.rt.Store.Get()
	if !s.CodebookEnabled {
		return 0
	}
	return s.CodebookSlots
}

// Tamper 返回规则引擎。
func (c *ProxyConfig) Tamper() proxy.TamperMatcher {
	if c.rt.tamper == nil {
		return nil
	}
	return c.rt.tamper
}

// Retry 返回重试策略。
func (c *ProxyConfig) Retry() proxy.RetryOptions {
	c.rt.Store.Refresh()
	s := c.rt.Store.Get()
	return proxy.RetryOptions{
		Enabled:      s.UpstreamRetryEnabled,
		MaxRetries:   s.UpstreamMaxRetries,
		DelaySeconds: s.UpstreamRetryDelaySeconds,
	}
}

// LoggerSink 把 proxy 的事件接到日志器。
type LoggerSink struct {
	Logger *logging.Logger
	// OnRequest 可选：接收请求记录（GUI 用）
	OnRequest func(proxy.RequestRecord)
	// CyberDir 用于写 cyber 事件日志
	CyberDir string
}

// Log 实现 proxy.EventSink。
func (s *LoggerSink) Log(level, msg string) {
	if s == nil || s.Logger == nil {
		return
	}
	switch level {
	case "warn":
		s.Logger.Warn("%s", msg)
	case "error":
		s.Logger.Error("%s", msg)
	default:
		s.Logger.Info("%s", msg)
	}
}

// Request 实现 proxy.EventSink。
func (s *LoggerSink) Request(rec proxy.RequestRecord) {
	if s == nil {
		return
	}
	if rec.Class == proxy.ClassFlagged && s.Logger != nil && s.CyberDir != "" {
		s.Logger.Cyber(s.CyberDir, map[string]any{
			"path":   rec.Path,
			"class":  rec.Class,
			"action": rec.Action,
			"note":   rec.Note,
		})
	}
	if s.OnRequest != nil {
		s.OnRequest(rec)
	}
}

// StartProxy 启动代理。已启动则返回现有引擎。
func (r *Runtime) StartProxy(ctx context.Context, opts proxy.Options) (*proxy.Engine, error) {
	r.mu.Lock()
	if r.engine != nil && r.engine.Running() {
		e := r.engine
		r.mu.Unlock()
		return e, nil
	}
	r.mu.Unlock()

	if opts.Config == nil {
		opts.Config = r.NewProxyConfig()
	}
	s := r.Store.Get()
	if opts.StreamWindowBytes <= 0 {
		opts.StreamWindowBytes = s.StreamWindowBytes
	}
	if opts.StreamWindowMs <= 0 {
		opts.StreamWindowMs = s.StreamWindowMs
	}
	// 观测层：调用方没显式给就用运行时持有的实例（UI 热切换同一个 Capture）
	if opts.Capture == nil {
		opts.Capture = r.capture
	}
	if opts.Recorder == nil {
		opts.Recorder = r.recorder
	}

	e, err := proxy.New(opts)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.engine = e
	r.mu.Unlock()

	return e, nil
}

// Capture 返回报文捕获器（可能为 nil）。
func (r *Runtime) Capture() *obs.Capture { return r.capture }

// Recorder 返回请求记录器（可能为 nil）。
func (r *Runtime) Recorder() *obs.Recorder { return r.recorder }

// RecordDir 返回请求记录目录。
func (r *Runtime) RecordDir() string {
	if r.recorder == nil {
		return ""
	}
	return filepath.Join(filepath.Dir(r.Store.Path()), "records")
}

// Engine 返回当前引擎（可能为 nil）。
func (r *Runtime) Engine() *proxy.Engine {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.engine
}

// SetSink 设置事件接收器。须在 StartProxy 之前调用。
func (r *Runtime) SetSink(s proxy.EventSink) { r.sink = s }

// Sink 返回当前事件接收器。
func (r *Runtime) Sink() proxy.EventSink { return r.sink }

// Close 释放资源。
func (r *Runtime) Close() {
	r.mu.Lock()
	e := r.engine
	r.mu.Unlock()
	if e != nil {
		_ = e.Stop()
	}
	if r.Logger != nil {
		_ = r.Logger.Close()
	}
}
