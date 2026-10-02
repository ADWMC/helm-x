package cli

import (
	"fmt"
	"io"

	"github.com/ADWMC/helm-x/internal/codexcfg"
	"github.com/ADWMC/helm-x/internal/config"
	"github.com/ADWMC/helm-x/internal/logging"
	"github.com/ADWMC/helm-x/internal/proxy"
	"github.com/ADWMC/helm-x/internal/runtime"
)

// newRuntime 构造 CLI 用的运行时。
//
// 日志目录取 codex home；找不到时退化为不写文件（仍可通过返回的 Runtime 取日志器）。
func newRuntime(stdout io.Writer) *runtime.Runtime {
	store := configStore()

	dir := ""
	if h, err := codexcfg.Find(); err == nil {
		dir = h.Dir
	}
	logger := logging.New(dir)

	rt := runtime.New(store, logger)
	rt.SetSink(&runtime.LoggerSink{
		Logger:   logger,
		CyberDir: dir,
		OnRequest: func(rec proxy.RequestRecord) {
			// CLI 模式下只把非正常判定打到 stdout，避免刷屏
			if rec.Class != proxy.ClassHealthy && rec.Class != "" {
				fmt.Fprintf(stdout, "[%s] %s action=%s %s\n",
					rec.Class, rec.Path, rec.Action, rec.Note)
			}
		},
	})
	return rt
}

// findHome 是 codexcfg.Find 的便捷包装。
func findHome() (codexcfg.Home, error) { return codexcfg.Find() }

// storePath 便于测试覆盖。
var storePath = config.Path
