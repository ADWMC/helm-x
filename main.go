// Command helmx 是 helm-x 的入口。
//
// 无参数 → 启动桌面图形界面（Wails + WebView2）。
// 带子命令 → 无窗口运行，供脚本与排查使用。
//
// 子命令与旧版保持一致（兼容既有用法）：
//
//	helmx proxy    本地映射代理
//	helmx apply    注入 codex 配置
//	helmx remove   还原并清理
//	helmx verify   自检
//	helmx watch    自愈守护
//	helmx ui       浏览器控制台（WebView2 不可用时的降级路径）
//
// 构建说明：
//   - `go build .` 产出的二进制**带桌面界面**（Wails 为纯 Go，无需额外步骤）
//   - 需要先构建前端：cd frontend && npm install && npm run build
//   - 前端缺失时窗口会显示提示页，命令行功能不受影响
package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"

	"github.com/ADWMC/helm-x/internal/cli"
	"github.com/ADWMC/helm-x/internal/svc"
)

// frontendDist 嵌入前端构建产物。
//
// all: 前缀是必需的：dist 里有以 _ 开头的文件，默认会被 go:embed 排除。
// 前端未构建时该目录只有占位文件，窗口会显示兜底提示页。
//
//go:embed all:frontend/dist
var frontendDist embed.FS

//go:embed build/appicon.png
var appIcon []byte

func main() {
	if len(os.Args) < 2 {
		if err := runGUI(); err != nil {
			fmt.Fprintf(os.Stderr, "helm-x: 界面启动失败: %v\n", err)
			fmt.Fprintln(os.Stderr, "提示：可用 `helmx proxy` 以无窗口模式运行。")
			os.Exit(1)
		}
		return
	}

	ctx, cancel := cli.SignalContext()
	defer cancel()
	os.Exit(cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

// runGUI 启动桌面窗口。
func runGUI() error {
	assets, err := fs.Sub(frontendDist, "frontend/dist")
	if err != nil {
		// 前端未构建：交给 svc 用内置兜底页
		assets = nil
	} else if _, statErr := fs.Stat(assets, "index.html"); statErr != nil {
		assets = nil
	}

	app, err := svc.New(svc.Options{
		Assets:      assets,
		Icon:        appIcon,
		WindowTitle: "helm-x 控制台",
	})
	if err != nil {
		return err
	}
	return app.Run()
}
