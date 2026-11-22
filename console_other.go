//go:build !windows

package main

// attachConsole 在非 Windows 平台是空操作：从终端启动时本来就有控制台。
func attachConsole() {}
