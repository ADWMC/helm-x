package cli

import (
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/ADWMC/helm-x/internal/selfcheck"
)

// runVerify 实现 `helmx verify`。
func runVerify(args []string, stdout, stderr io.Writer) error {
	e2e := false
	for _, a := range args {
		switch a {
		case "--e2e":
			e2e = true
		default:
			return &ExitError{Code: 2, Err: fmt.Errorf("未知选项 %q", a)}
		}
	}

	rep := selfcheck.Run(e2e, func(step, total int, c selfcheck.Check) {
		mark := "PASS"
		if c.Skip {
			mark = "SKIP"
		} else if !c.OK {
			mark = "FAIL"
		}
		fmt.Fprintf(stdout, "  [%d/%d] [%s] %s\n", step, total, mark, c.Name)
	})

	fmt.Fprintln(stdout)
	fmt.Fprint(stdout, rep.Text())
	if !rep.OK() {
		return &ExitError{Code: 1}
	}
	return nil
}

// runActivate 实现 `helmx activate`：跑一次 `codex exec "helmx"` 验证端到端。
func runActivate(_ []string, stdout, stderr io.Writer) error {
	fmt.Fprintln(stdout, "正在通过 codex 发送激活词（可能需要 1-2 分钟）…")

	// Windows 上 codex 是 npm 的 .cmd shim，必须经 cmd 转发
	cmd := exec.Command("cmd", "/c", `codex exec --skip-git-repo-check helmx 2>&1`)
	done := make(chan struct{})
	var out []byte
	var err error
	go func() {
		out, err = cmd.CombinedOutput()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(240 * time.Second):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return &ExitError{Code: 1, Err: fmt.Errorf("codex 执行超时（240s）")}
	}

	text := string(out)
	fmt.Fprintln(stdout, text)

	if err != nil {
		fmt.Fprintf(stderr, "codex 执行失败: %v\n", err)
		return &ExitError{Code: 1}
	}
	if strings.Contains(text, "helm-x online") || strings.Contains(text, "v45 online") {
		fmt.Fprintln(stdout, "[OK] 激活已确认")
		return nil
	}
	fmt.Fprintln(stderr, "[WARN] 回复中未检测到激活短语")
	return &ExitError{Code: 1}
}
