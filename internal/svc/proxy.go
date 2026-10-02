package svc

import (
	"context"
	"fmt"
	"github.com/ADWMC/helm-x/internal/config"
	"sync"

	"github.com/ADWMC/helm-x/internal/codexcfg"
	"github.com/ADWMC/helm-x/internal/proxy"
	"github.com/ADWMC/helm-x/internal/watch"
)

// ProxyService 暴露代理的启停与状态。
type ProxyService struct {
	s *Services

	mu     sync.Mutex
	cancel context.CancelFunc
}

// Status 返回完整状态（代理 + 配置 + 守护），供总览页一次拉齐。
func (p *ProxyService) Status() (StatusView, error) {
	ps, cs, ws := p.s.statusSnapshot()
	return StatusView{Proxy: ps, Codex: cs, Watch: ws}, nil
}

// StatusView 是总览页需要的全部状态。
type StatusView struct {
	Proxy ProxyStatus `json:"proxy"`
	Codex CodexState  `json:"codex"`
	Watch WatchStatus `json:"watch"`
}

// Start 启动代理。
//
// 启动前会自动把 codex 配置指向本代理 —— 与 CLI `helmx proxy` 行为一致，
// 否则用户点了"启动"但 codex 仍走原上游，会困惑。
func (p *ProxyService) Start() error {
	p.mu.Lock()
	if p.cancel != nil {
		p.mu.Unlock()
		return fmt.Errorf("代理已在运行")
	}
	p.mu.Unlock()

	ps, cs, _ := p.s.statusSnapshot()

	// 解析上游。
	//
	// 【为什么这段不能只靠 RelayURL()】
	// RelayURL() 只有在"配置已指向代理 **且** 还原点还在"时才能给出真实上游。
	// 但还原点是**一次性**的：RestoreProxy() 用完就删（backup.go:143）。
	//
	// 于是出现过一个死局（实测，日志 2026-10-01 18:48 起连续三次）：
	//   1. 某次启动写入代理地址 + 还原点
	//   2. 退出/手动还原消耗掉还原点，此时 base_url 可能仍是代理地址
	//   3. 再次启动：配置是代理态、还原点没了 → RelayURL 返回空 → 启动失败
	//   4. 且因为启动失败，用户无法从界面里恢复 —— 只能手改 config.toml
	//
	// 正确做法：**先把当前 base_url 存成新的还原点，再拿它当上游**。
	// 配置已经是代理态说明它此前是有效的上游地址，直接复用即可。
	// 这样即便还原点丢失也能自愈，不会把用户卡死。
	upstream := ps.Upstream

	if upstream == "" {
		if h, ok := p.s.rt.Home(); ok {
			// 路径 1：正常情况 —— 配置指向真实上游
			if relay, err := h.RelayURL(); err == nil && relay != "" {
				upstream = relay
			} else if ap, err := h.Probe(); err == nil && ap.BaseURL != "" && !ap.IsLocalProxy() {
				// 路径 2：RelayURL 失败但当前 base_url 本身就不是本地地址。
				// 直接用配置里的值。
				upstream = ap.BaseURL
			}
		}
	}

	if upstream == "" {
		// 走到这里说明 base_url 指向本地、又没有还原点 ——
		// 原始上游已无从得知。给出可执行的处置，而不是一句"无法确定"。
		return fmt.Errorf(
			"无法确定上游地址：codex 的 base_url 指向本地代理（%s），但没有可用的上游还原点。\n"+
				"请二选一：\n"+
				"  1. 在「服务」页执行「移除注入并还原」，恢复原始配置后重试\n"+
				"  2. 手动编辑 %s，把 base_url 改回真实上游地址",
			cs.BaseURL, cs.ConfigPath)
	}

	// 把 codex 指向本代理
	if h, ok := p.s.rt.Home(); ok {
		if _, err := h.Apply(codexcfg.ApplyOptions{ProxyAddr: p.s.rt.Store.ProxyBaseURL()}); err != nil {
			return fmt.Errorf("写入 codex 配置失败: %w", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	engine, err := p.s.rt.StartProxy(ctx, proxy.Options{
		Listen:   p.s.rt.Store.ListenAddress(),
		Upstream: upstream,
		Events:   p.s.rt.Sink(),
	})
	if err != nil {
		cancel()
		return err
	}

	p.mu.Lock()
	p.cancel = cancel
	p.mu.Unlock()

	go func() {
		if err := engine.Start(ctx); err != nil {
			p.s.rt.Logger.Error("代理异常退出: %v", err)
		}
		p.mu.Lock()
		p.cancel = nil
		p.mu.Unlock()
		p.s.emit(EventStatus, mustStatus(p))
	}()

	p.s.rt.Logger.Info("代理已启动：%s → %s", engine.Addr(), upstream)
	p.s.emit(EventStatus, mustStatus(p))
	return nil
}

// Stop 停止代理并**还原 codex 配置**。
//
// 还原是必须的：否则用户关掉界面后 codex 会指向已关闭的端口。
func (p *ProxyService) Stop() error {
	p.mu.Lock()
	cancel := p.cancel
	p.cancel = nil
	p.mu.Unlock()

	if cancel == nil {
		return fmt.Errorf("代理未在运行")
	}
	cancel()

	if e := p.s.rt.Engine(); e != nil {
		_ = e.Stop()
	}
	if h, ok := p.s.rt.Home(); ok {
		if done, err := h.RestoreProxy(); err != nil {
			p.s.rt.Logger.Warn("还原 codex 配置失败: %v", err)
		} else if done {
			p.s.rt.Logger.Info("codex 配置已还原")
		}
	}
	p.emitBoth()
	return nil
}

// Restart 重启代理。
func (p *ProxyService) Restart() error {
	if p.mu.TryLock() {
		p.mu.Unlock()
	}
	_ = p.Stop()
	return p.Start()
}

// SetPassthrough 切换透传模式（需重启生效）。
func (p *ProxyService) SetPassthrough(on bool) error {
	return p.s.rt.Store.Update(func(s *config.Settings) { s.Passthrough = on })
}

func (p *ProxyService) emitBoth() {
	p.s.emit(EventStatus, mustStatus(p))
	p.s.emit(EventConfigChg, nil)
}

func mustStatus(p *ProxyService) StatusView {
	v, err := p.Status()
	if err != nil {
		return StatusView{}
	}
	return v
}

// WatchStart 启动自愈守护。
func (p *ProxyService) WatchStart(intervalSec int) error {
	h, ok := p.s.rt.Home()
	if !ok {
		return fmt.Errorf("未找到 codex 配置目录")
	}
	p.s.mu.Lock()
	if p.s.watcher == nil {
		p.s.watcher = watch.New(h, intervalSec)
	}
	w := p.s.watcher
	p.s.mu.Unlock()

	w.Start(func(n int) {
		p.s.rt.Logger.Info("守护：检测到注入失效，已恢复（累计 %d 次）", n)
		p.s.emit(EventRestore, WatchStatus{Running: true, Restores: n})
		p.emitBoth()
	})
	p.emitBoth()
	return nil
}

// WatchStop 停止自愈守护。
func (p *ProxyService) WatchStop() error {
	p.s.mu.Lock()
	w := p.s.watcher
	p.s.mu.Unlock()
	if w != nil {
		w.Stop()
	}
	p.emitBoth()
	return nil
}
