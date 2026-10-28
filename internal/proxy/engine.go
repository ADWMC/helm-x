// engine.go 只负责引擎的生命周期与统计：构造、监听、停止、状态查询。
// 请求处理在 handler.go / stream.go，响应写出在 respond.go。

package proxy

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ADWMC/helm-x/internal/riskgate"
)

var (
	errNilView   = errors.New("proxy: request view 为空")
	errNotListen = errors.New("proxy: 未启动")
)

// Options 是引擎配置。
type Options struct {
	// Listen 是本地监听地址，如 "127.0.0.1:1800"。
	Listen string
	// Listener 是调用方**预先绑定**好的监听器。
	//
	// 为什么需要（2026-10 audit）：svc 层必须先确认端口可用、再改用户的
	// codex 配置。旧顺序是"先写配置再 listen"，端口被占时配置已指向一个
	// 起不来的端口，codex 直接不可用（RISKS R-7 的高频触发路径）。
	// 提供 Listener 后，绑定失败发生在写配置之前。
	Listener net.Listener
	// Upstream 是上游 base_url。
	Upstream string
	// Passthrough 为 true 时关闭全部内容转换（用于对照排查）。
	Passthrough bool
	// DryRun 只做计算不发上游（N6，调提示词/规则时不必真烧额度）。
	DryRun bool

	Retry RetryOptions

	// StreamWindowBytes / StreamWindowMs 是首段缓冲窗口（docs/PLAN.md §5.5）。
	// 窗口内可补救且用户无感；窗口结束后边读边转发。
	StreamWindowBytes int
	StreamWindowMs    int

	// InjectTail 为 true 时把注入指令放到 input 末尾而不是开头。
	InjectTail bool
	// Config 提供运行时可变配置（提示词模式等），可为 nil。
	Config ConfigProvider

	// Events 接收结构化事件，可为 nil。
	Events EventSink
}

// ConfigProvider 提供运行时配置。引擎每次请求读取，实现热生效（INV-5）。
type ConfigProvider interface {
	// PromptInstruction 返回当前应注入的指令；空串表示不注入。
	PromptInstruction() string
	// InjectEvery 返回注入频率：每 N 次请求注入 1 次（按会话计数）。
	// <=1 表示每次都注入。
	InjectEvery() int
	// FallbackUserAgent 返回 UA 兜底值：入站请求没有 User-Agent 时补上。
	// 空串 = 不补（此时不发送该头，避免泄漏 Go 默认 UA）。
	FallbackUserAgent() string
	// Rewriter 返回改写器；nil 表示未启用（跳过语义改写）。
	Rewriter() Rewriter
	// Gate 返回风险闸门；nil 表示未启用（完全不影响请求）。
	Gate() *riskgate.Gate
	// Tamper 返回拒绝规则引擎。
	Tamper() TamperMatcher
	// Retry 返回重试策略。
	Retry() RetryOptions
}

// EventSink 接收引擎产生的事件。
type EventSink interface {
	Log(level, msg string)
	Request(rec RequestRecord)
}

// RequestRecord 是一次请求的结构化记录（N4）。
// 字段与 frontend 的展示需求对应，见 docs/PLAN-SPEC.md §4。
type RequestRecord struct {
	ID         string    `json:"id"`
	TS         time.Time `json:"ts"`
	Path       string    `json:"path"`
	SessionID  string    `json:"sessionId,omitempty"`
	InBytes    int       `json:"inBytes"`
	OutBytes   int       `json:"outBytes"`
	Injected   bool      `json:"injected"`
	Class      Class     `json:"class"`
	Action     Action    `json:"action"`
	Upstream   int       `json:"upstreamHits"`
	DurationMs int64     `json:"durationMs"`
	Note       string    `json:"note,omitempty"`
	// Gate 是本次请求的风险闸门判定；nil 表示未启用或判定失败。
	Gate *riskgate.Verdict `json:"gate,omitempty"`
}

// Engine 是代理引擎。
type Engine struct {
	opts     Options
	upstream *Upstream

	// inject 是注入频率调度（按会话计数），见 inject.go。
	inject injectSched

	srv      *http.Server
	listener net.Listener

	running atomic.Bool
	seq     atomic.Uint64
	reqs    atomic.Uint64

	mu      sync.Mutex
	byClass map[Class]uint64
}

// New 构造引擎。
func New(opts Options) (*Engine, error) {
	if opts.Listen == "" {
		opts.Listen = "127.0.0.1:1800"
	}
	if opts.Upstream == "" {
		return nil, errors.New("proxy: 必须指定上游地址")
	}
	if opts.StreamWindowBytes <= 0 {
		opts.StreamWindowBytes = 2048
	}
	if opts.StreamWindowMs <= 0 {
		opts.StreamWindowMs = 400
	}
	if opts.Retry == (RetryOptions{}) {
		opts.Retry = DefaultRetryOptions()
	}

	up, err := NewUpstream(opts.Upstream)
	if err != nil {
		return nil, err
	}
	return &Engine{
		opts:     opts,
		upstream: up,
		byClass:  map[Class]uint64{},
	}, nil
}

// Start 开始监听并阻塞直到 ctx 结束。
func (e *Engine) Start(ctx context.Context) error {
	ln := e.opts.Listener
	if ln == nil {
		var err error
		ln, err = net.Listen("tcp", e.opts.Listen)
		if err != nil {
			return fmt.Errorf("监听 %s 失败: %w", e.opts.Listen, err)
		}
	}
	e.listener = ln
	e.running.Store(true)

	e.logf("info", "proxy: 监听 %s → %s", ln.Addr(), e.upstream.BaseURL())

	mux := http.NewServeMux()
	mux.HandleFunc("/", e.handle)

	e.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
		// 请求体上限 16MB，120s 足够；不设 WriteTimeout：流式响应可能很长
		ReadTimeout:    120 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20,
		// 把 net/http 自身的错误（握手失败、超大 header 等）接到事件流：
		// GUI 模式没有 stderr，不接就等于把 server 错误全丢掉（2026-10 audit）
		ErrorLog: log.New(&eventLogWriter{e: e}, "", 0),
	}

	errCh := make(chan error, 1)
	go func() {
		if err := e.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		e.running.Store(false)
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = e.srv.Shutdown(shutCtx)
		return nil
	case err := <-errCh:
		e.running.Store(false)
		return err
	}
}

// Stop 停止服务。
func (e *Engine) Stop() error {
	e.running.Store(false)
	if e.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return e.srv.Shutdown(ctx)
	}
	return nil
}

// Running 报告是否在监听。
func (e *Engine) Running() bool { return e.running.Load() }

// Addr 返回实际监听地址。
func (e *Engine) Addr() string {
	if e.listener != nil {
		return e.listener.Addr().String()
	}
	return e.opts.Listen
}

// UpstreamURL 返回上游地址。
func (e *Engine) UpstreamURL() string { return e.upstream.BaseURL() }

// Stats 返回累计统计。
type Stats struct {
	Requests uint64           `json:"requests"`
	ByClass  map[Class]uint64 `json:"byClass"`
	Running  bool             `json:"running"`
	Listen   string           `json:"listen"`
	Upstream string           `json:"upstream"`
}

// Stats 返回当前统计。
func (e *Engine) Stats() Stats {
	e.mu.Lock()
	defer e.mu.Unlock()
	cp := make(map[Class]uint64, len(e.byClass))
	for k, v := range e.byClass {
		cp[k] = v
	}
	return Stats{
		Requests: e.reqs.Load(),
		ByClass:  cp,
		Running:  e.running.Load(),
		Listen:   e.Addr(),
		Upstream: e.upstream.BaseURL(),
	}
}

func (e *Engine) countClass(c Class) {
	e.mu.Lock()
	e.byClass[c]++
	e.mu.Unlock()
}

func (e *Engine) logf(level, format string, args ...any) {
	if e.opts.Events != nil {
		e.opts.Events.Log(level, fmt.Sprintf(format, args...))
	}
}

// eventLogWriter 把 net/http 的 ErrorLog 输出接到事件流。
type eventLogWriter struct{ e *Engine }

func (w *eventLogWriter) Write(p []byte) (int, error) {
	if w != nil && w.e != nil {
		w.e.logf("error", "http: %s", strings.TrimSpace(string(p)))
	}
	return len(p), nil
}

func (e *Engine) emit(rec RequestRecord) {
	if e.opts.Events != nil {
		e.opts.Events.Request(rec)
	}
}

// handle 是唯一入口。
//
// 管线（docs/PLAN.md §4.2）：
//
//	Normalize → Inject → Send → Interpret → Remedy → Transmit
