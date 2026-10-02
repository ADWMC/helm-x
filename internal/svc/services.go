package svc

import (
	"context"
	"sync"
	"time"

	"github.com/ADWMC/helm-x/internal/assets"
	"github.com/ADWMC/helm-x/internal/codexcfg"
	"github.com/ADWMC/helm-x/internal/config"
	"github.com/ADWMC/helm-x/internal/proxy"
	"github.com/ADWMC/helm-x/internal/runtime"
	"github.com/ADWMC/helm-x/internal/watch"
)

// 事件名。前端 Events.On 依赖它们，改名前先读 docs/PLAN-SPEC.md §4。
const (
	EventLog       = "proxy:log"
	EventRequest   = "proxy:request"
	EventStatus    = "proxy:status"
	EventVerify    = "proxy:verify" // 自检进度
	EventRestore   = "proxy:restore"
	EventConfigChg = "config:changed"
)

// Emitter 把事件发给前端。由 app.go 提供实现，测试时可注入替身。
type Emitter interface {
	Emit(name string, data any)
}

// Services 汇总全部绑定服务，供 Wails 注册。
type Services struct {
	Proxy    *ProxyService
	Config   *ConfigService
	Prompt   *PromptService
	Rewriter *RewriterService
	Verify   *VerifyService
	Logs     *LogService
	QA       *QAService

	rt      *runtime.Runtime
	emitter Emitter
	watcher *watch.Watcher

	mu          sync.Mutex
	recent      []proxy.RequestRecord
	recentLimit int
}

// NewServices 构造服务容器。
func NewServices(rt *runtime.Runtime, emitter Emitter) *Services {
	s := &Services{
		rt:          rt,
		emitter:     emitter,
		recentLimit: 500,
	}
	s.Proxy = &ProxyService{s: s}
	s.Config = &ConfigService{s: s}
	s.Prompt = &PromptService{s: s}
	s.Rewriter = &RewriterService{s: s}
	s.Verify = &VerifyService{s: s}
	s.Logs = &LogService{s: s}
	s.QA = &QAService{s: s}

	rt.SetSink(&runtime.LoggerSink{
		Logger:    rt.Logger,
		CyberDir:  s.codexDir(),
		OnRequest: s.onRequest,
	})
	return s
}

func (s *Services) codexDir() string {
	if h, ok := s.rt.Home(); ok {
		return h.Dir
	}
	return ""
}

func (s *Services) emit(name string, data any) {
	if s.emitter != nil {
		s.emitter.Emit(name, data)
	}
}

// onRequest 接收代理的请求记录：缓存最近若干条并推给前端。
func (s *Services) onRequest(rec proxy.RequestRecord) {
	s.mu.Lock()
	s.recent = append(s.recent, rec)
	if len(s.recent) > s.recentLimit {
		s.recent = s.recent[len(s.recent)-s.recentLimit:]
	}
	s.mu.Unlock()

	s.emit(EventRequest, rec)
	if st, err := s.Proxy.Status(); err == nil {
		s.emit(EventStatus, st)
	}
}

// ── 前端可见的公共类型 ──

// ProxyStatus 是代理状态。
type ProxyStatus struct {
	Running  bool                   `json:"running"`
	Listen   string                 `json:"listen"`
	Upstream string                 `json:"upstream"`
	Requests uint64                 `json:"requests"`
	ByClass  map[proxy.Class]uint64 `json:"byClass"`
	Proxied  bool                   `json:"proxied"` // codex 是否已指向本代理
}

// CodexState 是 codex 配置状态。
type CodexState struct {
	Found          bool     `json:"found"`
	Home           string   `json:"home"`
	ConfigPath     string   `json:"configPath"`
	Provider       string   `json:"provider"`
	BaseURL        string   `json:"baseUrl"`
	Injected       bool     `json:"injected"`
	MissingKeys    []string `json:"missingKeys"`
	HasBackup      bool     `json:"hasBackup"`
	HasProxyBackup bool     `json:"hasProxyBackup"`
	Tables         int      `json:"tables"`
	Bytes          int      `json:"bytes"`
}

// WatchStatus 是自愈守护状态。
type WatchStatus struct {
	Running     bool   `json:"running"`
	IntervalSec int    `json:"intervalSec"`
	Restores    int    `json:"restores"`
	LastError   string `json:"lastError,omitempty"`
}

// statusSnapshot 组装当前状态，多个服务共用。
func (s *Services) statusSnapshot() (ProxyStatus, CodexState, WatchStatus) {
	var ps ProxyStatus
	if e := s.rt.Engine(); e != nil {
		st := e.Stats()
		ps = ProxyStatus{
			Running:  st.Running,
			Listen:   st.Listen,
			Upstream: st.Upstream,
			Requests: st.Requests,
			ByClass:  st.ByClass,
		}
	} else {
		ps.Listen = s.rt.Store.ListenAddress()
	}

	var cs CodexState
	if h, ok := s.rt.Home(); ok {
		cs.Found = true
		cs.Home = h.Dir
		cs.ConfigPath = h.ConfigPath()
		in := h.CheckInjection()
		cs.Injected = in.Injected
		cs.MissingKeys = in.MissingKeys
		cs.HasBackup = in.HasBackup
		cs.HasProxyBackup = in.HasProxyBackup
		cs.Provider = in.Provider
		cs.BaseURL = in.BaseURL
		if _, raw, err := h.ParseConfig(); err == nil {
			cs.Bytes = len(raw)
			if d, err2 := codexcfg.ParseDoc(raw); err2 == nil {
				cs.Tables = len(d.Tables())
			}
		}
		ps.Proxied = in.Proxied()
	}

	var ws WatchStatus
	if s.watcher != nil {
		w := s.watcher.Status()
		ws = WatchStatus{
			Running:   w.Running,
			Restores:  w.Restores,
			LastError: w.LastError,
		}
	}
	return ps, cs, ws
}

var _ = context.Background
var _ = time.Now
var _ = assets.PromptModes
var _ = config.Path
