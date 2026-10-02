package cli

import (
	"fmt"
	"io"
	"strconv"

	"github.com/ADWMC/helm-x/internal/codexcfg"
)

// runApply 实现 `helmx apply`：注入 codex 配置。
func runApply(args []string, stdout io.Writer) error {
	noProxy := false
	port := 1800

	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--no-proxy":
			noProxy = true
		case "--listen":
			v, err := nextArg(args, &i, "--listen")
			if err != nil {
				return &ExitError{Code: 2, Err: err}
			}
			p, cerr := strconv.Atoi(v)
			if cerr != nil || p <= 0 || p > 65535 {
				return &ExitError{Code: 2, Err: fmt.Errorf("--listen 需要 1-65535 的端口")}
			}
			port = p
		default:
			return &ExitError{Code: 2, Err: fmt.Errorf("未知选项 %q", a)}
		}
	}

	home, err := codexcfg.Find()
	if err != nil {
		return err
	}

	opts := codexcfg.ApplyOptions{}
	if !noProxy {
		opts.ProxyAddr = fmt.Sprintf("http://127.0.0.1:%d/v1", port)
	}

	res, err := home.Apply(opts)
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "配置文件   : %s\n", res.ConfigPath)
	if res.BackupMade {
		fmt.Fprintf(stdout, "已备份原文 : %s\n", res.BackupPath)
	} else {
		fmt.Fprintf(stdout, "备份       : %s（已存在，未覆盖）\n", res.BackupPath)
	}
	if len(res.InjectedKey) > 0 {
		fmt.Fprintf(stdout, "注入的键   : %v\n", res.InjectedKey)
	} else {
		fmt.Fprintln(stdout, "注入的键   : （均已存在）")
	}
	if res.Provider != "" {
		fmt.Fprintf(stdout, "激活 provider: %s\n", res.Provider)
	}
	if res.NewBaseURL != "" {
		fmt.Fprintf(stdout, "base_url   : %s → %s\n", res.OldBaseURL, res.NewBaseURL)
	}
	for _, w := range res.Warnings {
		fmt.Fprintf(stdout, "提示       : %s\n", w)
	}
	fmt.Fprintln(stdout, "\n完成。撤销请执行: helmx remove")
	return nil
}

// runRemove 实现 `helmx remove`：逐字节还原并清理。
func runRemove(args []string, stdout io.Writer) error {
	keep := false
	for _, a := range args {
		switch a {
		case "--keep-proxy-backup":
			keep = true
		default:
			return &ExitError{Code: 2, Err: fmt.Errorf("未知选项 %q", a)}
		}
	}

	home, err := codexcfg.Find()
	if err != nil {
		return err
	}

	res, err := home.Remove(codexcfg.RemoveOptions{KeepProxyBackup: keep})
	if err != nil {
		return err
	}
	if res.NothingToDo {
		fmt.Fprintln(stdout, "没有可还原的备份，无需操作")
		return nil
	}
	fmt.Fprintf(stdout, "已从备份还原: %s\n", res.ConfigPath)
	if res.ProxyBakKept {
		fmt.Fprintln(stdout, "base_url 还原点已保留")
	}
	return nil
}
