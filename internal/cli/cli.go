// Package cli 是命令行入口。
//
// 职责边界：
//   - 解析参数、组织各包的调用、格式化输出
//   - **不做业务判断**：注入逻辑在 codexcfg、代理在 proxy、自检在 selfcheck
//
// 子命令签名与旧版保持一致，保证既有用法可用（ROADMAP.md §4.1）。
//
// 文件职责：
//
//	cli.go      入口分发与 usage
//	proxy.go    helmx proxy
//	config.go   helmx apply / remove
//	verify.go   helmx verify / activate
//	watch.go    helmx watch
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/ADWMC/helm-x/internal/config"
)

// ExitError 携带退出码，供 Run 返回。
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("退出码 %d", e.Code)
	}
	return e.Err.Error()
}
func (e *ExitError) Unwrap() error { return e.Err }

// SignalContext 返回一个在 Ctrl+C / 进程被要求退出时取消的 context。
//
// 用于让代理在收到中断时能走完"还原 codex 配置"的收尾流程 ——
// 这是最坏的失败模式（用户 codex 被指向已关闭的代理），必须兜住。
func SignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// Run 执行子命令并返回退出码。
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stdout)
		return 0
	}

	cmd, rest := args[0], args[1:]

	// 任何子命令的 --help / -h 都要给帮助并退出 0（2026-10-28 体验审查：
	// apply/verify 报"未知选项"、proxy 只回一句"见 helmx help"，exit 2 —— 用户以为命令坏了）。
	if cmd != "help" && len(rest) > 0 {
		for _, a := range rest {
			if a == "--help" || a == "-h" {
				subUsage(stdout, cmd)
				return 0
			}
		}
	}

	var err error

	switch cmd {
	case "proxy":
		err = runProxy(ctx, rest, stdout, stderr)
	case "apply":
		err = runApply(rest, stdout)
	case "remove":
		err = runRemove(rest, stdout)
	case "verify":
		err = runVerify(rest, stdout, stderr)
	case "activate":
		err = runActivate(rest, stdout, stderr)
	case "watch":
		err = runWatch(ctx, rest, stdout)
	case "ui":
		err = runUI(ctx, rest, stdout)
	case "help", "-h", "--help":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "helmx: 未知子命令 %q\n\n", cmd)
		usage(stderr)
		return 2
	}

	if err != nil {
		var ee *ExitError
		if errors.As(err, &ee) {
			if ee.Err != nil {
				fmt.Fprintf(stderr, "helmx: %v\n", ee.Err)
			}
			return ee.Code
		}
		fmt.Fprintf(stderr, "helmx: %v\n", err)
		return 1
	}
	return 0
}

func usage(w io.Writer) {
	fmt.Fprint(w, `helm-x — codex 本地映射层

用法: helmx [子命令] [选项]

不带子命令时启动图形界面。

子命令:
  proxy              本地映射代理（无窗口，供脚本/排查使用）
  apply              注入 codex 配置
  remove             还原并清理
  verify             自检
  activate           发送激活词做端到端验证
  watch              自愈守护（前台）
  ui                 浏览器控制台（无 WebView2 时的降级路径）

proxy 选项:
  --listen PORT          监听端口，默认 1800
  --upstream URL         上游地址；缺省时从 codex 配置自动读取
  --max-retries N        失败后额外重试次数；0 表示无限（默认 10）
  --retry-delay SECONDS  固定重试间隔，默认 3
  --no-retry             本次运行禁用重试
  --passthrough          关闭全部内容转换（对照排查用）
  --dry-run              只计算不发上游
  --restore              手动还原 codex 配置后退出

apply 选项:
  --no-proxy             只注入上下文默认键，不改 base_url
  --listen PORT          写入配置的代理端口，默认 1800

verify 选项:
  --e2e                  额外执行一次 codex 激活验证（慢）

示例:
  helmx proxy --max-retries 0       无限重试
  helmx proxy --no-retry            禁用本次重试
  helmx apply && helmx verify
`)
}

// configStore 构造运行时配置。
func configStore() *config.Store {
	return config.NewStore(config.Path())
}

// subUsage 打印单个子命令的用法。未收录的子命令回落到总 help。
func subUsage(w io.Writer, cmd string) {
	switch cmd {
	case "proxy":
		fmt.Fprint(w, `用法: helmx proxy [选项]

  --listen PORT          监听端口，默认 1800
  --upstream URL         上游地址；缺省时从 codex 配置自动读取
  --max-retries N        失败后额外重试次数（与 --no-retry 互斥）
  --retry-delay SECONDS  重试间隔，默认 3
  --no-retry             本次运行禁用重试（优先于配置文件）
  --passthrough          关闭全部内容转换（对照排查用）
  --dry-run              只计算不发上游，且不改写 codex 配置
  --restore              还原 codex 配置后退出
  --help, -h             显示本帮助

按 Ctrl+C 退出时会自动还原 codex 配置。
`)
	case "apply":
		fmt.Fprint(w, `用法: helmx apply [选项]

  --no-proxy             只注入上下文默认键，不改 base_url
  --listen PORT          写入配置的代理端口，默认 1800
  --help, -h             显示本帮助
`)
	case "remove":
		fmt.Fprint(w, `用法: helmx remove [选项]

  --keep-proxy-backup    保留 base_url 还原点
  --force                允许用旧备份整文件覆盖当前配置（会丢后加的键）
  --help, -h             显示本帮助
`)
	case "verify":
		fmt.Fprint(w, `用法: helmx verify [选项]

  --e2e                  额外跑一次 codex 激活验证
  --help, -h             显示本帮助
`)
	case "activate":
		fmt.Fprint(w, `用法: helmx activate

发送激活词做端到端验证。
`)
	case "watch":
		fmt.Fprint(w, `用法: helmx watch

前台自愈守护：代理掉线时自动拉起。
`)
	case "ui":
		fmt.Fprint(w, `用法: helmx ui [选项]

  --listen PORT          控制台端口，默认自动选择
  --help, -h             显示本帮助
`)
	default:
		usage(w)
	}
}
