package runtime

import (
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/ADWMC/helm-x/internal/codexcfg"
)

// healStaleProxy 处理"上次被强杀，codex 还指着一个没人监听的本地端口"。
//
// 为什么需要（2026-11-22）：托盘方案废弃后，正常关窗会走 shutdown 还原配置；
// 但任务管理器结束进程 / 断电 / 崩溃不会 —— `config.toml` 停在
// `http://127.0.0.1:<port>/v1`，下一次 codex 直接 ECONNREFUSED，且报错里不出现
// "helm-x"。以前只能靠 `helmx verify` 第 7 项事后发现，现在启动时自愈。
//
// 判定条件（三个同时满足才动手，避免误改用户配置）：
//  1. 激活 provider 的 base_url 指向本机回环地址；
//  2. 那个端口上没有任何进程在监听（800ms 内连不上）；
//  3. 存在 base_url 还原点（否则无从还原，只能提示）。
func healStaleProxy(h codexcfg.Home, logf func(level, format string, args ...any)) {
	probe, err := h.Probe()
	if err != nil || probe.BaseURL == "" {
		return
	}
	host, port, ok := localListen(probe.BaseURL)
	if !ok {
		return // 指向外部上游：正常状态
	}
	if dialOK(host, port) {
		return // 本地代理在跑：正常状态
	}
	if !h.HasProxyBackup() {
		logf("warn", "codex 的 base_url 指向 %s 但该端口无人监听，且没有还原点；执行 helmx proxy --restore 或重新启动代理", probe.BaseURL)
		return
	}
	done, rerr := h.RestoreProxy()
	switch {
	case rerr != nil:
		logf("warn", "上次异常退出留下的本地 base_url 还原失败: %v；执行 helmx proxy --restore 手工修复", rerr)
	case done:
		logf("info", "检测到上次异常退出（base_url 指向无人的本地端口），已还原 codex 配置")
	}
}

// localListen 判断 URL 是否指向本机回环地址，返回 host/port。
func localListen(rawURL string) (host, port string, ok bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", false
	}
	h := u.Hostname()
	if h != "localhost" && h != "::1" && !strings.HasPrefix(h, "127.") {
		return "", "", false
	}
	p := u.Port()
	if p == "" {
		return "", "", false
	}
	return h, p, true
}

// dialOK 尝试 TCP 连接（800ms）。
func dialOK(host, port string) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 800*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
