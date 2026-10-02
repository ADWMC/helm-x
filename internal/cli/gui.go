package cli

import (
	"fmt"
	"os/exec"
	"runtime"
)

// openBrowser 尽力打开系统浏览器。失败不报错 —— 用户可手动访问打印出的地址。
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// RunGUI 启动桌面界面。
//
// 这是个占位实现：Wails 集成在 internal/svc + wails.json 中，
// 需要 `wails3 build` 才能产出带窗口的二进制。
// 直接 `go run .` 时回退到浏览器控制台，保证任何构建方式下都有可用界面。
func RunGUI() error {
	fmt.Println("helm-x: 未以 Wails 方式构建，回退到浏览器控制台")
	fmt.Println("       如需桌面窗口，请使用 wails3 dev / wails3 build")

	ctx, cancel := SignalContext()
	defer cancel()
	return runUI(ctx, []string{"--port", "8090"}, stdoutWriter{})
}

type stdoutWriter struct{}

func (stdoutWriter) Write(p []byte) (int, error) { return fmt.Print(string(p)) }
