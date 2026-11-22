// Package netenv 决定出站 HTTP 走不走代理（从 helm-a 迁入，2026-11-22）。
//
// 现实约束：Windows 上「系统代理」（设置 → 网络 → 代理，或 Clash / Verge 等工具
// 写进注册表的那一项）**不等于**进程环境变量；Go 的 http.ProxyFromEnvironment
// 只读环境变量。因此在只配了系统代理的机器上，Go 会直连公网 —— 被墙/被企业网关
// 拦截时表现为 dial timeout，而错误信息里没有任何"代理"字样（helm-a 真机首跑即如此）。
//
// 解析顺序：环境变量 → Windows 系统代理（HKCU 注册表）→ 直连。
package netenv

import (
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

var (
	once   sync.Once
	proxyF func(*http.Request) (*url.URL, error)
)

// ProxyFunc 返回用于 http.Transport.Proxy 的函数（惰性探测一次，进程内缓存）。
func ProxyFunc() func(*http.Request) (*url.URL, error) {
	once.Do(func() { proxyF = resolve() })
	return proxyF
}

// HTTPClient 返回带代理与超时的客户端。
func HTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{Proxy: ProxyFunc()},
	}
}

func resolve() func(*http.Request) (*url.URL, error) {
	if hasEnvProxy() {
		return http.ProxyFromEnvironment
	}
	if p := systemProxy(); p != "" {
		if u, err := url.Parse(withScheme(p)); err == nil {
			return http.ProxyURL(u)
		}
	}
	return http.ProxyFromEnvironment // 无代理（含 NO_PROXY 命中）
}

func hasEnvProxy() bool {
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		if strings.TrimSpace(os.Getenv(k)) != "" {
			return true
		}
	}
	return false
}

// systemProxy 读 Windows 系统代理；其它平台返回空。
func systemProxy() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	out, err := exec.Command("reg", "query",
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`,
		"/v", "ProxyServer").Output()
	if err != nil {
		return ""
	}
	// 形如：    ProxyServer    REG_SZ    127.0.0.1:7897
	// 也可能是 "http=host:port;https=host:port"
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "ProxyServer") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		raw := fields[len(fields)-1]
		if strings.Contains(raw, "=") {
			for _, part := range strings.Split(raw, ";") {
				kv := strings.SplitN(part, "=", 2)
				if len(kv) == 2 && strings.EqualFold(kv[0], "https") && kv[1] != "" {
					return kv[1]
				}
			}
			for _, part := range strings.Split(raw, ";") {
				kv := strings.SplitN(part, "=", 2)
				if len(kv) == 2 && kv[1] != "" {
					return kv[1]
				}
			}
			continue
		}
		if raw != "" {
			return raw
		}
	}
	return ""
}

func withScheme(p string) string {
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") || strings.HasPrefix(p, "socks5://") {
		return p
	}
	return "http://" + p
}
