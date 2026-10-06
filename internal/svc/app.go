package svc

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/ADWMC/helm-x/internal/codexcfg"
	"github.com/ADWMC/helm-x/internal/config"
	"github.com/ADWMC/helm-x/internal/logging"
	"github.com/ADWMC/helm-x/internal/runtime"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// Options 是桌面应用的构造参数。
type Options struct {
	// Assets 是前端构建产物（frontend/dist）。
	Assets fs.FS
	// Icon 是窗口图标，可为 nil。
	Icon []byte
	// WindowTitle 默认 "helm-x 控制台"。
	WindowTitle string
}

// App 是桌面应用。
//
// **这是唯一依赖 Wails 的文件之一**（另一个是 main 的 GUI 入口）。
// 领域包全部零 Wails 依赖，因此 beta 版本 API 变动只影响此处（RISKS.md R-4）。
type App struct {
	wails    *application.App
	window   *application.WebviewWindow
	rt       *runtime.Runtime
	svc      *Services
	icon     []byte
	quitting bool
}

// New 构造桌面应用（不启动）。
func New(opts Options) (*App, error) {
	if opts.WindowTitle == "" {
		opts.WindowTitle = "helm-x 控制台"
	}

	// 运行时：日志写到 codex home，与 CLI 共用同一份日志文件
	dir := ""
	if h, err := codexcfg.Find(); err == nil {
		dir = h.Dir
	}
	logger := logging.New(dir)
	store := config.NewStore(config.Path())
	rt := runtime.New(store, logger)

	a := &App{rt: rt, icon: opts.Icon}

	// 前端资源：优先用传入的 dist，其次用 go:embed 的内置兜底页
	assets := opts.Assets
	if assets == nil {
		assets = fallbackAssets
	}

	app := application.New(application.Options{
		Name:        "helm-x",
		Description: "codex 本地映射层",
		Icon:        opts.Icon,
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets),
		},
		Logger:   slog.Default(),
		LogLevel: slog.LevelWarn,
		// 单实例：第二次启动拉起已有窗口，而不是端口冲突
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "helm-x-single-instance",
		},
		// 退出时务必还原 codex 配置 —— 这是最坏的失败模式
		OnShutdown: func() {
			a.shutdown()
		},
		// 关窗口不退出（最小化到托盘），真正的退出走托盘菜单
		Windows: application.WindowsOptions{
			DisableQuitOnLastWindowClosed: true,
		},
	})

	a.wails = app
	a.svc = NewServices(rt, wailsEmitter{app: app})

	// 注册服务：方法签名即前端调用名
	app.RegisterService(application.NewService(a.svc.Proxy))
	app.RegisterService(application.NewService(a.svc.Config))
	app.RegisterService(application.NewService(a.svc.Prompt))
	app.RegisterService(application.NewService(a.svc.Rewriter))
	app.RegisterService(application.NewService(a.svc.Verify))
	app.RegisterService(application.NewService(a.svc.Logs))
	app.RegisterService(application.NewService(a.svc.QA))

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     opts.WindowTitle,
		Width:     1280,
		Height:    820,
		MinWidth:  960,
		MinHeight: 600,
		URL:       "/",
	})
	a.window = win

	a.setupTray()
	a.setupWindowEvents()
	return a, nil
}

// Run 启动应用（阻塞）。
func (a *App) Run() error {
	// 自动启动代理：用户打开界面就是为了用它
	if err := a.svc.Proxy.Start(); err != nil {
		a.rt.Logger.Warn("自动启动代理失败: %v", err)
	}
	return a.wails.Run()
}

// Services 返回服务容器，供测试或扩展使用。
func (a *App) Services() *Services { return a.svc }

// shutdown 是退出收尾。
//
// **必须**：还原 codex 的 base_url。否则用户关掉界面后 codex 会指向已关闭的端口，
// 表现为"codex 完全不可用" —— 这是本项目最坏的失败模式（RISKS.md R-7）。
func (a *App) shutdown() {
	if a.quitting {
		return
	}
	a.quitting = true

	if e := a.rt.Engine(); e != nil {
		_ = e.Stop()
	}
	if h, ok := a.rt.Home(); ok {
		if done, err := h.RestoreProxy(); err != nil {
			a.rt.Logger.Error("退出时还原 codex 配置失败: %v", err)
		} else if done {
			a.rt.Logger.Info("退出时已还原 codex 配置")
		}
	}
	a.rt.Close()
}

// wailsEmitter 把事件发给前端。
type wailsEmitter struct{ app *application.App }

// Emit 实现 Emitter。Wails 的 Emit 返回是否成功，这里丢弃 ——
// 前端未订阅（例如窗口未打开）不应影响后端逻辑。
func (e wailsEmitter) Emit(name string, data any) {
	e.app.Event.Emit(name, data)
}

// fallbackAssets 是前端未构建时的兜底页，避免打开窗口一片空白。
//
//go:embed fallback/index.html
var fallbackAssets embed.FS

var _ = http.StatusOK
