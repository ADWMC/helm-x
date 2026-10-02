package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/ADWMC/helm-x/internal/codexcfg"
	"github.com/ADWMC/helm-x/internal/watch"
)

// runWatch 实现 `helmx watch`：前台自愈守护。
//
// 周期检查注入状态，被改坏就恢复。与旧版 watch 行为一致。
func runWatch(ctx context.Context, args []string, stdout io.Writer) error {
	interval := 60
	if len(args) > 0 {
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 5 {
			return &ExitError{Code: 2, Err: fmt.Errorf("间隔需要 ≥5 的整数秒")}
		}
		interval = n
	}

	home, err := codexcfg.Find()
	if err != nil {
		return err
	}

	w := watch.New(home, interval)
	fmt.Fprintf(stdout, "[helm-x] 守护已启动，间隔 %ds —— Ctrl+C 停止\n", interval)

	w.RunForeground(ctx.Done(), func(st watch.Status) {
		ts := time.Now().Format("15:04:05")
		switch {
		case st.LastError != "":
			fmt.Fprintf(stdout, "[%s] 检查异常: %s\n", ts, st.LastError)
		case st.Restores > 0 && !st.LastRestore.IsZero() && time.Since(st.LastRestore) < time.Duration(interval)*time.Second:
			fmt.Fprintf(stdout, "[%s] 检测到注入失效，已恢复（累计 %d 次）\n", ts, st.Restores)
		}
	})

	fmt.Fprintln(stdout, "[helm-x] 守护已停止")
	return nil
}
