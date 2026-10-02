// Package watch 是自愈守护。
//
// 职责：周期性检查 codex 配置的注入状态，发现被改坏就恢复。
// 不负责启动代理 —— 那是 proxy 的事。
package watch

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/ADWMC/helm-x/internal/codexcfg"
)

// Status 是守护的当前状态。
type Status struct {
	Running     bool      `json:"running"`
	IntervalSec int       `json:"intervalSec"`
	Restores    int       `json:"restores"`
	LastRestore time.Time `json:"lastRestore,omitzero"`
	LastCheck   time.Time `json:"lastCheck,omitzero"`
	LastError   string    `json:"lastError,omitempty"`
}

// Watcher 是自愈守护。
type Watcher struct {
	home     codexcfg.Home
	interval time.Duration

	mu      sync.Mutex
	stop    chan struct{}
	done    chan struct{}
	running atomic.Bool

	restores    atomic.Int64
	lastRestore atomic.Int64
	lastCheck   atomic.Int64
	lastErr     atomic.Value // string
}

// New 构造守护。interval 小于 5 秒会被抬到 5 秒（与旧版一致）。
func New(home codexcfg.Home, intervalSec int) *Watcher {
	if intervalSec < 5 {
		intervalSec = 5
	}
	return &Watcher{
		home:     home,
		interval: time.Duration(intervalSec) * time.Second,
	}
}

// Start 启动后台循环。已在运行则是空操作。
func (w *Watcher) Start(onRestore func(count int)) {
	w.mu.Lock()
	if w.running.Load() {
		w.mu.Unlock()
		return
	}
	w.stop = make(chan struct{})
	w.done = make(chan struct{})
	w.running.Store(true)
	stop := w.stop
	done := w.done
	w.mu.Unlock()

	go func() {
		defer close(done)
		t := time.NewTicker(w.interval)
		defer t.Stop()
		for {
			w.pass(onRestore)
			select {
			case <-stop:
				w.running.Store(false)
				return
			case <-t.C:
			}
		}
	}()
}

// Stop 停止循环并等待退出。
func (w *Watcher) Stop() {
	w.mu.Lock()
	if !w.running.Load() {
		w.mu.Unlock()
		return
	}
	stop, done := w.stop, w.done
	w.mu.Unlock()

	close(stop)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}
	w.running.Store(false)
}

// pass 执行一轮检查与恢复。
func (w *Watcher) pass(onRestore func(int)) {
	w.lastCheck.Store(time.Now().Unix())

	st := w.home.CheckInjection()
	if !st.ConfigExists {
		w.lastErr.Store("config.toml 不存在")
		return
	}
	if st.Injected {
		w.lastErr.Store("")
		return
	}

	// 注入被改坏：尝试恢复
	if err := w.home.EnsureInjectKeys(); err != nil {
		w.lastErr.Store(err.Error())
		return
	}
	if after := w.home.CheckInjection(); after.Injected {
		n := int(w.restores.Add(1))
		w.lastRestore.Store(time.Now().Unix())
		w.lastErr.Store("")
		if onRestore != nil {
			onRestore(n)
		}
	} else {
		w.lastErr.Store("恢复后仍未通过检查")
	}
}

// Status 返回当前状态。
func (w *Watcher) Status() Status {
	s := Status{
		Running:  w.running.Load(),
		Restores: int(w.restores.Load()),
	}
	if v := w.lastRestore.Load(); v > 0 {
		s.LastRestore = time.Unix(v, 0)
	}
	if v := w.lastCheck.Load(); v > 0 {
		s.LastCheck = time.Unix(v, 0)
	}
	if v, ok := w.lastErr.Load().(string); ok {
		s.LastError = v
	}
	return s
}

// RunForeground 是 CLI `helmx watch` 的前台阻塞模式。
func (w *Watcher) RunForeground(stop <-chan struct{}, onPass func(Status)) {
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		w.pass(nil)
		if onPass != nil {
			onPass(w.Status())
		}
		select {
		case <-stop:
			return
		case <-t.C:
		}
	}
}
