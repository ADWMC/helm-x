package svc

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// setupWindowEvents 处理窗口生命周期。
//
// 行为：关闭窗口 → 隐藏到托盘，而不是退出。
// 理由：代理是常驻服务，关窗口就停代理会让 codex 立刻断掉。
// 真正退出走托盘菜单的"退出"。
func (a *App) setupWindowEvents() {
	a.window.OnWindowEvent(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if a.quitting {
			return
		}
		// 取消关闭，改为隐藏
		e.Cancel()
		a.window.Hide()
		a.rt.Logger.Info("窗口已隐藏到托盘；代理仍在运行")
	})
}

// setupTray 建立托盘菜单。
func (a *App) setupTray() {
	tray := a.wails.SystemTray.New()
	tray.SetLabel("helm-x")
	tray.SetTooltip("helm-x — codex 本地映射层")

	menu := a.wails.NewMenu()
	menu.Add("显示控制台").OnClick(func(ctx *application.Context) {
		a.window.Show()
		a.window.Focus()
	})
	menu.AddSeparator()
	menu.Add("启动代理").OnClick(func(ctx *application.Context) {
		if err := a.svc.Proxy.Start(); err != nil {
			a.rt.Logger.Warn("托盘启动代理失败: %v", err)
		}
	})
	menu.Add("停止代理").OnClick(func(ctx *application.Context) {
		if err := a.svc.Proxy.Stop(); err != nil {
			a.rt.Logger.Warn("托盘停止代理失败: %v", err)
		}
	})
	menu.AddSeparator()
	menu.Add("还原 codex 配置并退出").OnClick(func(ctx *application.Context) {
		a.quit()
	})

	tray.SetMenu(menu)
	tray.OnClick(func() {
		a.window.Show()
		a.window.Focus()
	})
}

// quit 走完整的退出流程：还原配置 → 退出。
func (a *App) quit() {
	a.shutdown()
	a.wails.Quit()
}
