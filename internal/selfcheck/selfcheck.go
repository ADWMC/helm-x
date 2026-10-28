// Package selfcheck 是内置自检。
//
// 职责：只读地检查运行环境与注入状态，产出一份报告。
// **不修改任何文件** —— 修复动作属于 codexcfg / CLI 的 apply。
//
// 检查项与旧版 verify（verify.cpp）对齐，但按实测调整了判据。
package selfcheck

import (
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/ADWMC/helm-x/internal/assets"
	"github.com/ADWMC/helm-x/internal/codexcfg"
)

// Check 是一项检查结果。
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Skip   bool   `json:"skip,omitempty"`
}

// Report 是自检报告。
type Report struct {
	Checks []Check `json:"checks"`
	Failed int     `json:"failed"`
}

// OK 报告是否全部通过（跳过项不算失败）。
func (r Report) OK() bool { return r.Failed == 0 }

// Text 返回可读报告。
func (r Report) Text() string {
	var sb strings.Builder
	sb.WriteString("helm-x 自检\n")
	sb.WriteString("==========\n")
	for _, c := range r.Checks {
		mark := "PASS"
		if c.Skip {
			mark = "SKIP"
		} else if !c.OK {
			mark = "FAIL"
		}
		fmt.Fprintf(&sb, "  [%s] %-32s %s\n", mark, c.Name, c.Detail)
	}
	sb.WriteString("==========\n")
	if r.OK() {
		sb.WriteString("全部通过\n")
	} else {
		fmt.Fprintf(&sb, "%d 项失败\n", r.Failed)
	}
	return sb.String()
}

// Progress 在每项检查完成时回调，供 GUI 展示进度。
type Progress func(step, total int, c Check)

// Run 执行全部检查。e2e 为 true 时额外跑一次 codex 激活验证。
func Run(e2e bool, progress Progress) Report {
	var r Report
	total := 7
	if e2e {
		total = 8
	}
	step := 0
	add := func(c Check) {
		step++
		r.Checks = append(r.Checks, c)
		if !c.OK && !c.Skip {
			r.Failed++
		}
		if progress != nil {
			progress(step, total, c)
		}
	}

	// 1. codex home
	home, err := codexcfg.Find()
	if err != nil {
		add(Check{Name: "codex 配置目录", OK: false,
			Detail: "未找到；设置 CODEX_HOME 或先运行一次 codex"})
		// 后续检查依赖它，全部跳过
		for i := 1; i < total; i++ {
			add(Check{Name: "（依赖前一项）", OK: true, Skip: true, Detail: "已跳过"})
		}
		return r
	}
	add(Check{Name: "codex 配置目录", OK: true, Detail: home.Dir})

	// 2. 配置文件可读且可解析
	d, raw, err := home.ParseConfig()
	if err != nil {
		add(Check{Name: "config.toml 可解析", OK: false, Detail: err.Error()})
	} else {
		add(Check{Name: "config.toml 可解析", OK: true,
			Detail: fmt.Sprintf("%d 字节 / %d 个表", len(raw), len(d.Tables()))})
	}

	// 3. 激活 provider
	ap, perr := home.Probe()
	if perr != nil {
		add(Check{Name: "激活 provider", OK: false, Detail: perr.Error()})
	} else {
		add(Check{Name: "激活 provider", OK: true,
			Detail: fmt.Sprintf("%s → %s", ap.Name, ap.BaseURL)})
	}

	// 4. 注入状态
	st := home.CheckInjection()
	if st.Injected {
		add(Check{Name: "配置注入状态", OK: true, Detail: "三个上下文键齐备"})
	} else {
		add(Check{Name: "配置注入状态", OK: false,
			Detail: "缺少: " + strings.Join(st.MissingKeys, ", ")})
	}

	// 5. 资源完整性
	//
	// 先跑 HealthCheck：它会逐条尝试解密全部嵌入资源，失败时带出可执行原因
	// （典型是"构建时用了 HELMX_ASSET_KEY，当前环境没设置/值不同"）。
	// 只在数量上判断会漏掉这种情况 —— 解密失败时 PromptModes 为空、TamperRules 为空，
	// 用户只看到"提示词 0 个模式"，不知道该去设环境变量。
	if err := assets.HealthCheck(); err != nil {
		add(Check{Name: "内嵌资源", OK: false, Detail: "资源不可用: " + err.Error()})
	} else {
		// 健康之后再核对内容非空，保证数量口径与 HealthCheck 互不替代：
		// HealthCheck 管"能不能解密"，这里管"解出来是不是真有东西"。
		prompts := assets.PromptModes()
		tamperLen := len(assets.TamperRules())
		ok := len(prompts) > 0 && tamperLen > 0
		// 带上密钥来源：内置开发种子构建的包和 HELMX_ASSET_KEY 构建的包行为不同，
		// 排障时第一时间要能看出来手里的二进制是哪一种。
		add(Check{Name: "内嵌资源", OK: ok,
			Detail: fmt.Sprintf("提示词 %d 个模式，TAMPER 规则 %d 字节，密钥来源 %s",
				len(prompts), tamperLen, assets.KeySource())})
	}

	// 6. 备份
	switch {
	case st.HasBackup:
		add(Check{Name: "配置备份", OK: true, Detail: home.BakPath()})
	case st.Injected:
		add(Check{Name: "配置备份", OK: false, Detail: "已注入但无备份 —— 无法自动还原"})
	default:
		add(Check{Name: "配置备份", OK: true, Skip: true, Detail: "尚未注入，无需备份"})
	}

	// 7. 代理态一致性（2026-10-28 用户体验审查的阻断项）
	//
	// 为什么必须有：`apply` 单独执行、或代理被强杀，都会把 base_url 留在
	// 127.0.0.1:<port> 上。此时 codex 只会报 ECONNREFUSED，文本里不出现 helm-x，
	// 而自检仍报"全部通过" —— 工具把自己搞坏了却说自己没事。
	if up, uerr := home.Probe(); uerr == nil {
		if host, port, ok := localListenAddr(up.BaseURL); ok {
			if connErr := dialLocal(host, port); connErr != nil {
				add(Check{Name: "代理态一致", OK: false,
					Detail: fmt.Sprintf("base_url 指向 %s，但本机连不上（%v）；执行 helmx remove 还原，或重新启动代理", up.BaseURL, connErr)})
			} else {
				add(Check{Name: "代理态一致", OK: true, Detail: "代理在监听 " + up.BaseURL})
			}
		} else {
			add(Check{Name: "代理态一致", OK: true, Skip: true, Detail: "base_url 不是本机地址，无需检查"})
		}
	} else {
		add(Check{Name: "代理态一致", OK: true, Skip: true, Detail: "无激活 provider，已跳过"})
	}

	// 8. e2e
	if e2e {
		out, err := runCodex()
		switch {
		case err != nil:
			add(Check{Name: "e2e codex 激活", OK: false, Detail: err.Error()})
		case strings.Contains(out, "helm-x online") || strings.Contains(out, "v45 online"):
			add(Check{Name: "e2e codex 激活", OK: true, Detail: "激活词已确认"})
		default:
			add(Check{Name: "e2e codex 激活", OK: false, Detail: "未在回复中检测到激活短语"})
		}
	}
	return r
}

// runCodex 执行 `codex exec "helmx"` 并捕获输出。
//
// Windows 上 codex 是 npm 的 .cmd shim，必须经 cmd 转发（旧版 verify.cpp:126 同）。
func runCodex() (string, error) {
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
		return string(out), err
	case <-time.After(240 * time.Second):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return "", fmt.Errorf("codex 执行超时（240s）")
	}
}

// localListenAddr 判断 base_url 是否指向本机端口，返回 host/port。
// 只认 localhost / 127.0.0.x / ::1 —— 外部地址不属于"代理态"。
func localListenAddr(baseURL string) (host, port string, ok bool) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", "", false
	}
	h := u.Hostname()
	if h != "localhost" && h != "127.0.0.1" && h != "::1" && !strings.HasPrefix(h, "127.") {
		return "", "", false
	}
	p := u.Port()
	if p == "" {
		return "", "", false
	}
	return h, p, true
}

// dialLocal 尝试 TCP 连接，超时 800ms。
func dialLocal(host, port string) error {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 800*time.Millisecond)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}
