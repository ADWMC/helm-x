//go:build windows

package main

import (
	"os"
	"syscall"
)

// attachConsole 把本进程挂到父进程的控制台上，并重建 stdout/stderr。
//
// 为什么需要（2026-11-22）：GUI 版必须以 `-H windowsgui` 链接，否则双击 exe
// 会先弹一个黑色控制台窗口，看着不像桌面软件。但同一个 exe 还提供
// `helmx proxy / verify / apply` 这些命令行子命令 —— GUI 子系统的进程没有
// 控制台，命令行输出会直接消失（用户看到"命令没反应"）。
//
// AttachConsole(ATTACH_PARENT_PROCESS) 把进程挂到调用它的终端上，
// 再打开 CONOUT$ 替换 os.Stdout/os.Stderr，命令行用法就恢复正常。
// 从资源管理器双击时没有父控制台，AttachConsole 失败 —— 正好保持无窗口。
func attachConsole() {
	const attachParentProcess = ^uintptr(0) // (DWORD)-1

	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	attach := kernel32.NewProc("AttachConsole")
	if r, _, _ := attach.Call(attachParentProcess); r == 0 {
		return // 没有父控制台：GUI 启动路径，什么都不做
	}

	// 关键：从终端启动时，stdout/stderr 已经是**有效的继承句柄**（可能是管道，
	// 例如 PowerShell 的 `&` 会把它接成管道来捕获输出）。这种情况下必须原样保留，
	// 覆盖成 CONOUT$ 会绕过重定向，`helmx verify | ...` 就什么都收不到
	// （2026-11-22 实测：只 AttachConsole 不判断句柄，输出直接消失）。
	//
	// 只有当句柄无效（进程由 GUI 子系统启动、没有任何 std 句柄）时才重建。
	if _, err := os.Stdout.Stat(); err != nil {
		if f, ferr := os.OpenFile("CONOUT$", os.O_WRONLY, 0); ferr == nil {
			os.Stdout = f
			os.Stderr = f
		}
	}
	if _, err := os.Stdin.Stat(); err != nil {
		if f, ferr := os.OpenFile("CONIN$", os.O_RDONLY, 0); ferr == nil {
			os.Stdin = f
		}
	}
}
