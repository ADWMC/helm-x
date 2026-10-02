// Package config 管理 %APPDATA%\helmx.config.json。
//
// 职责：读写用户设置、提供运行时热重载的配置快照。
// 不做业务判断 —— 提示词注入、改写、重试策略由 proxy 决定。
//
// 字段名与旧版保持一致，保证既有用户的配置直接可用（ROADMAP.md §4.1）。
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Settings 是全部用户设置。字段名 = 旧版 JSON 字段名。
type Settings struct {
	// 代理
	ListenPort  int    `json:"listen_port,omitempty"`
	PromptMode  string `json:"prompt_mode"`
	Passthrough bool   `json:"passthrough,omitempty"`

	// 上游重试（语义与旧版一致：MaxRetries 是额外次数，0 = 无限）
	UpstreamRetryEnabled      bool `json:"upstream_retry_enabled"`
	UpstreamMaxRetries        int  `json:"upstream_max_retries"`
	UpstreamRetryDelaySeconds int  `json:"upstream_retry_delay_seconds"`

	// 流式首段缓冲窗口（新版新增，旧版读到会忽略）
	StreamWindowBytes int `json:"stream_window_bytes,omitempty"`
	StreamWindowMs    int `json:"stream_window_ms,omitempty"`

	// 改写器
	Rewriter RewriterSettings `json:"rewriter"`

	// 已废弃的 Context Gardener 字段。
	// 旧配置里可能存在：**读取时忽略、不报错、不再写回**（FINDINGS-phase1 §5）。
	// 用 RawMessage 接收以免旧字段导致解析失败。
	LegacyContextGardenerEnabled   json.RawMessage `json:"context_gardener_enabled,omitempty"`
	LegacyContextGardenerThreshold json.RawMessage `json:"context_gardener_threshold_bytes,omitempty"`
}

// RewriterSettings 是改写器配置。
type RewriterSettings struct {
	Enabled      bool   `json:"enabled"`
	Provider     string `json:"provider,omitempty"`
	BaseURL      string `json:"base_url,omitempty"`
	APIKey       string `json:"api_key,omitempty"`
	Model        string `json:"model,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	TimeoutSec   int    `json:"timeout_sec,omitempty"`
	UseProxy     bool   `json:"use_proxy,omitempty"`
	ProxyURL     string `json:"proxy_url,omitempty"`
	Fallback     string `json:"fallback,omitempty"`     // none(默认) | local
	MaxAttempts  int    `json:"max_attempts,omitempty"` // 默认 3
}

// Default 返回默认设置，与旧版默认值一致。
func Default() Settings {
	return Settings{
		ListenPort:                1800,
		PromptMode:                "default",
		UpstreamRetryEnabled:      true,
		UpstreamMaxRetries:        10,
		UpstreamRetryDelaySeconds: 3,
		StreamWindowBytes:         2048,
		StreamWindowMs:            400,
		Rewriter: RewriterSettings{
			Enabled:     false,
			Provider:    "klapi",
			BaseURL:     "https://klapi.me/v1",
			Model:       "mimo-v2.5-pro",
			TimeoutSec:  90,
			ProxyURL:    "http://127.0.0.1:7897",
			Fallback:    "none",
			MaxAttempts: 3,
		},
	}
}

// Path 返回配置文件路径（%APPDATA%\helmx.config.json）。
func Path() string {
	if v := os.Getenv("APPDATA"); v != "" {
		return filepath.Join(v, "helmx.config.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "helmx.config.json"
	}
	return filepath.Join(home, ".helmx.config.json")
}

// Store 持有当前设置，支持热重载（INV-5）。
//
// 行为与旧版一致：UI 保存后**立即生效**，无需重启代理。
// 旧版每请求重新解析 JSON（proxy.cpp:884）；新版改为 mtime 检查 + 主动 Swap。
type Store struct {
	path string
	cur  atomic.Pointer[Settings]

	mu        sync.Mutex
	lastMTime time.Time
	lastSize  int64
}

// NewStore 从磁盘加载（文件不存在则用默认值）。
func NewStore(path string) *Store {
	if path == "" {
		path = Path()
	}
	s := &Store{path: path}
	d := Default()
	s.cur.Store(&d)
	s.reload(true)
	return s
}

// Path 返回配置文件路径。
func (s *Store) Path() string { return s.path }

// ListenAddress 返回形如 "127.0.0.1:1800" 的监听地址。
//
// 只监听回环地址：这是本地映射层，不应对局域网暴露。
func (s *Store) ListenAddress() string {
	p := s.Get().ListenPort
	if p <= 0 {
		p = Default().ListenPort
	}
	return fmt.Sprintf("127.0.0.1:%d", p)
}

// ProxyBaseURL 返回应写入 codex 配置的地址。
func (s *Store) ProxyBaseURL() string {
	return "http://" + s.ListenAddress() + "/v1"
}

// Get 返回当前设置的副本。
func (s *Store) Get() Settings {
	p := s.cur.Load()
	if p == nil {
		return Default()
	}
	return *p
}

// Refresh 检查文件是否变化，变化则重载。每次请求调用一次即可（成本极低）。
func (s *Store) Refresh() {
	s.reload(false)
}

func (s *Store) reload(force bool) {
	st, err := os.Stat(s.path)
	if err != nil {
		return // 文件不存在：保持当前值（默认或上次的）
	}
	s.mu.Lock()
	unchanged := !force && st.ModTime().Equal(s.lastMTime) && st.Size() == s.lastSize
	if !unchanged {
		s.lastMTime = st.ModTime()
		s.lastSize = st.Size()
	}
	s.mu.Unlock()
	if unchanged {
		return
	}

	raw, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	cur := Default()
	// 先铺默认值再覆盖，保证缺字段时不会变成零值
	if err := json.Unmarshal(raw, &cur); err != nil {
		return // 解析失败：保持当前值，不破坏可用性
	}
	s.normalize(&cur)
	s.cur.Store(&cur)
}

// normalize 补齐缺省值，保证数值合法。
func (s *Store) normalize(v *Settings) {
	d := Default()
	if v.ListenPort <= 0 || v.ListenPort > 65535 {
		v.ListenPort = d.ListenPort
	}
	if v.PromptMode == "" {
		v.PromptMode = d.PromptMode
	}
	if v.UpstreamRetryDelaySeconds <= 0 {
		v.UpstreamRetryDelaySeconds = d.UpstreamRetryDelaySeconds
	}
	if v.UpstreamMaxRetries < 0 {
		v.UpstreamMaxRetries = 0 // 0 = 无限，合法
	}
	if v.StreamWindowBytes <= 0 {
		v.StreamWindowBytes = d.StreamWindowBytes
	}
	if v.StreamWindowMs <= 0 {
		v.StreamWindowMs = d.StreamWindowMs
	}
	if v.Rewriter.TimeoutSec <= 0 {
		v.Rewriter.TimeoutSec = d.Rewriter.TimeoutSec
	}
	if v.Rewriter.MaxAttempts <= 0 {
		v.Rewriter.MaxAttempts = d.Rewriter.MaxAttempts
	}
	if v.Rewriter.Fallback == "" {
		v.Rewriter.Fallback = d.Rewriter.Fallback
	}
}

// Save 写回磁盘并立即生效。
//
// 写入是原子的（先临时文件再替换），避免 UI 保存过程中崩溃留下半个文件。
func (s *Store) Save(v Settings) error {
	s.normalize(&v)

	// 保留旧配置里的 Context Gardener 字段原样回写？
	// 不保留：已废弃字段不再写回，但读取时也不报错（避免"升级即报错"）。
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(s.path, append(raw, '\n')); err != nil {
		return err
	}

	s.cur.Store(&v)
	s.mu.Lock()
	if st, err := os.Stat(s.path); err == nil {
		s.lastMTime = st.ModTime()
		s.lastSize = st.Size()
	}
	s.mu.Unlock()
	return nil
}

// Update 在锁内做读-改-写，供 UI 局部更新使用。
func (s *Store) Update(mutate func(*Settings)) error {
	v := s.Get()
	mutate(&v)
	return s.Save(v)
}

func atomicWrite(path string, content []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".helmx-cfg-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)

	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
