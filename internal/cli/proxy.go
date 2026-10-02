package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/ADWMC/helm-x/internal/codexcfg"
	"github.com/ADWMC/helm-x/internal/proxy"
	"github.com/ADWMC/helm-x/internal/runtime"
)

// runProxy 实现 `helmx proxy`。
//
// 启动流程（与旧版一致）：
//  1. 确定上游（--upstream 或从 codex 配置自动读）
//  2. 把 codex 的 base_url 指向本代理
//  3. 起服务，阻塞直到中断
//  4. **收尾时还原 codex 配置** —— 这是最坏的失败模式，必须兜住
func runProxy(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	opts, err := parseProxyArgs(args)
	if err != nil {
		return &ExitError{Code: 2, Err: err}
	}

	rt := newRuntime(stdout)
	defer rt.Close()

	home, hasHome := rt.Home()

	if opts.restore {
		if !hasHome {
			fmt.Fprintln(stdout, "helmx: 未找到 codex 配置目录")
			return &ExitError{Code: 1}
		}
		done, rerr := home.RestoreProxy()
		if rerr != nil {
			return rerr
		}
		if !done {
			fmt.Fprintln(stdout, "codex 配置本就未指向本地代理，无需还原")
			return nil
		}
		fmt.Fprintln(stdout, "codex 配置已还原")
		return nil
	}

	// 1. 确定上游
	upstream := opts.upstream
	if upstream == "" {
		if !hasHome {
			return &ExitError{Code: 2, Err: fmt.Errorf("未找到 codex 配置且未指定 --upstream")}
		}
		relay, rerr := home.RelayURL()
		if rerr != nil || relay == "" {
			return &ExitError{Code: 2, Err: fmt.Errorf("codex 配置里没有可用的 base_url，请用 --upstream 指定")}
		}
		upstream = relay
		fmt.Fprintf(stdout, "[helm-x] 自动读取上游: %s\n", relay)
	}

	// 2. 把 codex 指向本代理
	proxyURL := fmt.Sprintf("http://127.0.0.1:%d/v1", opts.listen)
	if hasHome {
		res, aerr := home.Apply(codexcfg.ApplyOptions{ProxyAddr: proxyURL})
		if aerr != nil {
			return fmt.Errorf("写入 codex 配置失败: %w", aerr)
		}
		if res.ProxyBak != "" {
			fmt.Fprintf(stdout, "[helm-x] 已保存上游还原点: %s\n", res.ProxyBak)
		}
		fmt.Fprintf(stdout, "[helm-x] codex 配置: %s → %s\n", res.Provider, proxyURL)
	} else {
		fmt.Fprintln(stdout, "[helm-x] 警告: 未找到 codex 配置，仅启动代理")
	}

	// 3. 起服务
	engine, err := rt.StartProxy(ctx, proxy.Options{
		Listen:      fmt.Sprintf("127.0.0.1:%d", opts.listen),
		Upstream:    upstream,
		Passthrough: opts.passthrough,
		DryRun:      opts.dryRun,
		Retry:       opts.retry,
		Events:      rt.Sink(),
	})
	if err != nil {
		return err
	}

	printProxyBanner(stdout, engine, opts)

	// 收到中断后先把配置还原，再退出
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- engine.Start(runCtx) }()

	select {
	case <-ctx.Done():
		fmt.Fprintln(stdout, "\n[helm-x] 正在停止…")
	case err := <-errCh:
		restoreHome(rt, stdout)
		return err
	}

	cancel()
	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
	}
	restoreHome(rt, stdout)
	fmt.Fprintln(stdout, "[helm-x] 已停止")
	return nil
}

// restoreHome 还原 codex 的 base_url。
//
// 三重兜底思路（旧版也是三重，proxy.cpp:1412-1417）：
// 信号处理 → 正常退出 → 手动 --restore。
func restoreHome(rt *runtime.Runtime, stdout io.Writer) {
	home, ok := rt.Home()
	if !ok {
		return
	}
	done, err := home.RestoreProxy()
	switch {
	case err != nil:
		fmt.Fprintf(stdout, "[helm-x] 警告: 还原 codex 配置失败: %v\n", err)
		fmt.Fprintln(stdout, "[helm-x] 可手动执行: helmx proxy --restore")
	case done:
		fmt.Fprintln(stdout, "[helm-x] codex 配置已还原")
	}
}

func printProxyBanner(w io.Writer, e *proxy.Engine, o proxyArgs) {
	retry := "已禁用"
	if o.retry.Enabled {
		if o.retry.MaxRetries == 0 {
			retry = fmt.Sprintf("无限次，间隔 %ds", o.retry.DelaySeconds)
		} else {
			retry = fmt.Sprintf("额外 %d 次，间隔 %ds", o.retry.MaxRetries, o.retry.DelaySeconds)
		}
	}
	fmt.Fprint(w, "==============================================\n")
	fmt.Fprintf(w, "  helm-x 代理\n")
	fmt.Fprintf(w, "  监听    : %s\n", e.Addr())
	fmt.Fprintf(w, "  上游    : %s\n", e.UpstreamURL())
	fmt.Fprintf(w, "  重试    : %s\n", retry)
	mode := "注入 ON · TAMPER ON"
	if o.passthrough {
		mode = "透传模式（不做任何转换）"
	} else if o.dryRun {
		mode = "干跑模式（不发上游）"
	}
	fmt.Fprintf(w, "  模式    : %s\n", mode)
	fmt.Fprint(w, "  按 Ctrl+C 停止\n")
	fmt.Fprint(w, "==============================================\n")
}

type proxyArgs struct {
	listen      int
	upstream    string
	passthrough bool
	dryRun      bool
	restore     bool
	retry       proxy.RetryOptions
	// 以下标记 CLI 是否显式覆盖了配置
	maxSet   bool
	delaySet bool
}

func parseProxyArgs(args []string) (proxyArgs, error) {
	o := proxyArgs{listen: 1800, retry: proxy.DefaultRetryOptions()}
	sawMax, sawNoRetry := false, false

	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--listen":
			v, err := nextArg(args, &i, "--listen")
			if err != nil {
				return o, err
			}
			p, err := strconv.Atoi(v)
			if err != nil || p <= 0 || p > 65535 {
				return o, fmt.Errorf("--listen 需要 1-65535 的端口")
			}
			o.listen = p
		case "--upstream":
			v, err := nextArg(args, &i, "--upstream")
			if err != nil {
				return o, err
			}
			o.upstream = strings.TrimSpace(v)
		case "--max-retries":
			v, err := nextArg(args, &i, "--max-retries")
			if err != nil {
				return o, err
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return o, fmt.Errorf("--max-retries 需要非负整数")
			}
			sawMax = true
			o.maxSet = true
			o.retry.Enabled = true
			o.retry.MaxRetries = n
		case "--retry-delay":
			v, err := nextArg(args, &i, "--retry-delay")
			if err != nil {
				return o, err
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				return o, fmt.Errorf("--retry-delay 需要正整数")
			}
			o.delaySet = true
			o.retry.DelaySeconds = n
		case "--no-retry":
			sawNoRetry = true
			o.retry.Enabled = false
		case "--passthrough":
			o.passthrough = true
		case "--dry-run":
			o.dryRun = true
		case "--restore":
			o.restore = true
		case "--help", "-h":
			return o, fmt.Errorf("见 helmx help")
		default:
			return o, fmt.Errorf("未知选项 %q", a)
		}
	}

	if sawMax && sawNoRetry {
		return o, fmt.Errorf("--max-retries 与 --no-retry 不能同时使用")
	}
	return o, nil
}

func nextArg(args []string, i *int, name string) (string, error) {
	if *i+1 >= len(args) {
		return "", fmt.Errorf("%s 缺少参数值", name)
	}
	*i++
	return args[*i], nil
}
