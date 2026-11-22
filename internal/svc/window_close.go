package svc

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// setupWindowEvents 在窗口关闭**之前**把 codex 配置还原。
//
// 为什么需要（2026-11-22 实测）：点关闭后进程正常退出，但 `OnShutdown` 并没有
// 被调到 —— 日志里没有「退出时已还原」，`config.toml` 停在 `127.0.0.1:1800/v1`，
// 下一次 codex 直接 ECONNREFUSED。所以在 WindowClosing 里同步还原一次；
// `shutdown()` 自身幂等（`quitting` 闸门），与 OnShutdown 重复调用无害。
//
// 注意：这里**不要** `e.Cancel()`。取消关闭在本 Wails beta 上会毁出 202×56 残窗
// （原 HWND 消失，Show/UnMinimise/ShowWindow(SW_RESTORE) 都恢复不了），
// "关窗缩托盘"那条路已废弃，需要无窗口常驻时用 `helmx proxy`。
func (a *App) setupWindowEvents() {
	a.window.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.shutdown()
	})
}
