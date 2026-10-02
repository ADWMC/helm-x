package svc

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ADWMC/helm-x/internal/codexcfg"
	"github.com/ADWMC/helm-x/internal/config"
	"github.com/ADWMC/helm-x/internal/logging"
	"github.com/ADWMC/helm-x/internal/runtime"
)

// 最小可用的 codex 配置：一个 provider 指向给定 base_url。
func writeConfig(t *testing.T, dir, baseURL string) codexcfg.Home {
	t.Helper()
	body := "model_provider = \"p\"\n\n[model_providers.p]\n" +
		"name = \"p\"\nbase_url = \"" + baseURL + "\"\nwire_api = \"responses\"\n"
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return codexcfg.Home{Dir: dir}
}

// freePort 要一个当前空闲的端口号。
//
// 【为何不能直接用默认的 1800】
// 首版测试用默认配置（监听 127.0.0.1:1800），只要本机正在跑一个真实的
// helmx.exe，测试就必然失败 —— 表现为"启动后 3 秒内仍未进入运行状态"。
// 那是我在开发机上亲身踩到的：测试红了，但代码没问题。
// 让测试自己找空闲端口，就不再依赖外部环境。
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func newTestServices(t *testing.T, home codexcfg.Home) *Services {
	t.Helper()
	store := config.NewStore(filepath.Join(t.TempDir(), "helmx.config.json"))

	// 指到一个空闲端口，避免与开发机上正在运行的实例冲突
	cfg := config.Default()
	cfg.ListenPort = freePort(t)
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}

	logger := logging.New(t.TempDir())
	rt := runtime.New(store, logger)
	rt.SetHome(home)
	t.Cleanup(rt.Close)
	return NewServices(rt, nil)
}

// ★ 回归：还原点被消耗后，再次启动必须仍能自愈。
//
// 曾经的死局（实测，日志 2026-10-01 18:48 起连续三次失败）：
//
//  1. 启动写入代理地址 + 还原点
//  2. 退出/手动还原消耗掉还原点，base_url 可能仍是代理地址
//  3. 再次启动：配置是代理态、还原点没了 → RelayURL 返回空 → 启动失败
//  4. 且启动失败导致用户无法从界面恢复，只能手改 config.toml
//
// 修法：RelayURL 失败时，如果当前 base_url 不是本地地址就直接用它；
// 若确实是本地地址且无还原点，给出可执行的处置说明而不是一句"无法确定"。
func TestStartRejectsLocalBaseURLWithoutBackup(t *testing.T) {
	dir := t.TempDir()
	// 配置指向本地代理，且没有还原点 —— 原始上游无从得知
	home := writeConfig(t, dir, "http://127.0.0.1:1800/v1")
	s := newTestServices(t, home)

	err := s.Proxy.Start()
	if err == nil {
		t.Fatal("指向本地且无还原点时应拒绝启动，而不是盲猜上游")
	}

	msg := err.Error()
	// 错误信息必须可执行：说明原因 + 给出两条出路
	for _, want := range []string{"127.0.0.1:1800", "移除注入并还原", "config.toml"} {
		if !strings.Contains(msg, want) {
			t.Errorf("错误信息缺少可执行指引 %q，实得：\n%s", want, msg)
		}
	}
}

// 正常路径：配置指向真实上游时能启动，并记录正确的上游。
//
// 注意：Start() 在 goroutine 里拉起 engine.Start()，返回时监听尚未就绪。
// 因此不能读完 Status() 立刻断言 Running —— 要轮询等待。
func TestStartWithRealUpstream(t *testing.T) {
	dir := t.TempDir()
	home := writeConfig(t, dir, "https://upstream.example/v1")
	s := newTestServices(t, home)

	if err := s.Proxy.Start(); err != nil {
		t.Fatalf("应能启动: %v", err)
	}
	t.Cleanup(func() { _ = s.Proxy.Stop() })

	running := waitRunning(t, s, 3*time.Second)
	if !running {
		t.Fatal("启动后 3 秒内仍未进入运行状态")
	}

	st, err := s.Proxy.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.Proxy.Upstream != "https://upstream.example/v1" {
		t.Errorf("上游 = %q, want https://upstream.example/v1", st.Proxy.Upstream)
	}
}

// waitRunning 轮询等待代理进入运行状态。
func waitRunning(t *testing.T, s *Services, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st, err := s.Proxy.Status()
		if err == nil && st.Proxy.Running {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// ★ 核心回归：还原点丢失但 base_url 已是代理态时，不应卡死。
//
// 这条覆盖的场景是：先启动（写入代理地址 + 还原点），
// 然后还原点被删掉（模拟 RestoreProxy 消耗、或用户手工删除），
// 此时 base_url 仍指向代理。旧实现会永久启动失败。
func TestStartSurvivesMissingBackupWhenConfigNotLocal(t *testing.T) {
	dir := t.TempDir()
	// base_url 是真实上游、没有还原点 —— 这是"还原后再次启动"的正常状态
	home := writeConfig(t, dir, "https://upstream.example/v1")
	s := newTestServices(t, home)

	// 第一次启动
	if err := s.Proxy.Start(); err != nil {
		t.Fatalf("首次启动失败: %v", err)
	}
	_ = s.Proxy.Stop()

	// 删掉还原点，模拟它被消耗
	_ = os.Remove(home.ProxyBakPath())

	// 第二次启动必须仍能工作
	if err := s.Proxy.Start(); err != nil {
		t.Fatalf("还原点缺失后应仍能启动（自愈），实得: %v", err)
	}
	t.Cleanup(func() { _ = s.Proxy.Stop() })
}
