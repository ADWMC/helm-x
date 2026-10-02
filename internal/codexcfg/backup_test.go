package codexcfg

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestHome 在临时目录建一个 codex home，写入给定内容。
func newTestHome(t *testing.T, content string) Home {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ConfigName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return Home{Dir: dir}
}

// ★ INV-6 的核心断言：apply → remove 后逐字节一致。
func TestByteRoundTrip(t *testing.T) {
	origins := map[string]string{
		"真实结构": fixture,
		"CRLF": "model = \"a\"\r\n\r\n[model_providers.x]\r\nbase_url = \"https://up.invalid/v1\"\r\n",
		"带注释":  "# 我的注释\nmodel = \"a\" # 行尾\n\n[model_providers.x] # 表注释\nbase_url = \"https://up.invalid/v1\"\n",
		"空文件":  "",
		"只有表头": "[model_providers.x]\nbase_url = \"https://up.invalid/v1\"\n",
	}

	for name, origin := range origins {
		t.Run(name, func(t *testing.T) {
			h := newTestHome(t, origin)

			// apply：改 base_url + 注入三个上下文键
			err := h.Update(func(d *Doc) error {
				if err := d.SetString([]string{"model_providers", "x"}, "base_url", "http://127.0.0.1:1800/v1"); err != nil {
					if !strings.Contains(err.Error(), "未找到") {
						return err
					}
				}
				for _, kv := range ContextDefaultKeys {
					if err := d.InsertTopLevelRaw(kv.Key, []byte(kv.Val)); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				t.Fatalf("Update: %v", err)
			}

			after := readFile(t, h.ConfigPath())
			if bytes.Equal(after, []byte(origin)) {
				t.Fatal("apply 之后内容没变化，测试未生效")
			}
			if !h.HasBackup() {
				t.Fatal("apply 之后应存在备份")
			}

			// remove：从备份还原
			if err := h.RestoreFromBackup(); err != nil {
				t.Fatalf("RestoreFromBackup: %v", err)
			}

			got := readFile(t, h.ConfigPath())
			if !bytes.Equal(got, []byte(origin)) {
				t.Errorf("还原后不是逐字节一致:\n got %q\nwant %q", got, origin)
			}
			if h.HasBackup() {
				t.Error("还原后备份应被清理")
			}
		})
	}
}

func readFile(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// 备份一旦存在就不得被覆盖，否则回不到最初状态。
func TestBackupNotOverwritten(t *testing.T) {
	h := newTestHome(t, `model = "v1"
[model_providers.x]
base_url = "https://one.invalid/v1"
`)
	if _, err := h.EnsureBackup(); err != nil {
		t.Fatal(err)
	}
	first := readFile(t, h.BakPath())

	// 改两次文件，备份必须保持第一次的内容
	if err := os.WriteFile(h.ConfigPath(), []byte("model = \"v2\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.EnsureBackup(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, h.BakPath()); !bytes.Equal(got, first) {
		t.Errorf("备份被覆盖:\n got %q\nwant %q", got, first)
	}
}

func TestRestoreWithoutBackupFails(t *testing.T) {
	h := newTestHome(t, "model = \"a\"\n")
	err := h.RestoreFromBackup()
	if err == nil {
		t.Fatal("无备份时还原应报错")
	}
}

func TestUpdateRejectsUnsupportedForm(t *testing.T) {
	h := newTestHome(t, "[[products]]\nname = \"a\"\n")
	err := h.Update(func(d *Doc) error { return nil })
	if err == nil {
		t.Fatal("不支持的形态应拒绝写入")
	}
	// 文件必须原样未动
	if got := readFile(t, h.ConfigPath()); string(got) != "[[products]]\nname = \"a\"\n" {
		t.Errorf("拒绝写入时文件被改动: %q", got)
	}
}

func TestUpdateDoesNotWriteOnMutateError(t *testing.T) {
	origin := "model = \"a\"\n"
	h := newTestHome(t, origin)
	sentinel := ErrKeyNotFound
	err := h.Update(func(d *Doc) error { return sentinel })
	if err == nil {
		t.Fatal("mutate 报错时 Update 应失败")
	}
	if got := readFile(t, h.ConfigPath()); string(got) != origin {
		t.Errorf("mutate 失败却写入了: %q", got)
	}
	if h.HasBackup() {
		t.Error("mutate 失败不应产生备份")
	}
}

// 写入必须是原子的：中途失败不能留下半个文件。
// 通过检查临时文件被清理来间接验证。
func TestAtomicWriteLeavesNoTempFiles(t *testing.T) {
	h := newTestHome(t, "model = \"a\"\n")
	if err := h.Update(func(d *Doc) error {
		return d.InsertTopLevel("added", "x")
	}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".helmx-tmp-") {
			t.Errorf("残留临时文件: %s", e.Name())
		}
	}
}

func TestProbeActiveProvider(t *testing.T) {
	h := newTestHome(t, fixture)
	ap, err := h.Probe()
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if ap.Name != "1145" {
		t.Errorf("Name = %q, want 1145", ap.Name)
	}
	if ap.BaseURL != "https://example.invalid/v1" {
		t.Errorf("BaseURL = %q", ap.BaseURL)
	}
	if ap.WireAPI != "responses" {
		t.Errorf("WireAPI = %q", ap.WireAPI)
	}
	if ap.IsLocalProxy() {
		t.Error("外部地址不应判为本地代理")
	}
}

func TestProbeNoActiveProvider(t *testing.T) {
	h := newTestHome(t, "model = \"a\"\n")
	if _, err := h.Probe(); err == nil {
		t.Fatal("没有 model_provider 时应报错")
	}
}

// 代理态下 RelayURL 应从还原点取原始地址。
func TestRelayURLInProxyState(t *testing.T) {
	h := newTestHome(t, `model = "m"
model_provider = "x"

[model_providers.x]
base_url = "https://real-upstream.invalid/v1"
`)
	// 存还原点
	if err := h.SnapshotProxyBak(); err != nil {
		t.Fatal(err)
	}
	// 切到代理态
	if err := h.Update(func(d *Doc) error {
		return d.SetString([]string{"model_providers", "x"}, "base_url", "http://127.0.0.1:1800/v1")
	}); err != nil {
		t.Fatal(err)
	}

	relay, err := h.RelayURL()
	if err != nil {
		t.Fatalf("RelayURL: %v", err)
	}
	if relay != "https://real-upstream.invalid/v1" {
		t.Errorf("RelayURL = %q, want 原始上游", relay)
	}
}

func TestRelayURLWhenNotProxied(t *testing.T) {
	h := newTestHome(t, fixture)
	relay, err := h.RelayURL()
	if err != nil {
		t.Fatal(err)
	}
	if relay != "https://example.invalid/v1" {
		t.Errorf("RelayURL = %q", relay)
	}
}

func TestCheckInjection(t *testing.T) {
	t.Run("未注入", func(t *testing.T) {
		h := newTestHome(t, fixture)
		st := h.CheckInjection()
		if st.Injected {
			t.Error("应报告未注入")
		}
		if len(st.MissingKeys) != 3 {
			t.Errorf("MissingKeys = %v, want 3 项", st.MissingKeys)
		}
		if st.Provider != "1145" {
			t.Errorf("Provider = %q", st.Provider)
		}
	})

	t.Run("已注入", func(t *testing.T) {
		h := newTestHome(t, fixture)
		if err := h.Update(func(d *Doc) error {
			for _, kv := range ContextDefaultKeys {
				if err := d.InsertTopLevelRaw(kv.Key, []byte(kv.Val)); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		st := h.CheckInjection()
		if !st.Injected {
			t.Errorf("应报告已注入，MissingKeys=%v", st.MissingKeys)
		}
		if !st.HasBackup {
			t.Error("应报告有备份")
		}
	})
}

// 部分注入也要被识别为"未完成注入"。
func TestCheckInjectionPartial(t *testing.T) {
	src := "tool_output_token_limit = 8000\nmodel_provider = \"x\"\n\n[model_providers.x]\nbase_url = \"https://a.invalid/v1\"\n"
	h := newTestHome(t, src)
	st := h.CheckInjection()
	if st.Injected {
		t.Error("只注入一个键不应算已完成")
	}
	if len(st.MissingKeys) != 2 {
		t.Errorf("MissingKeys = %v, want 2 项", st.MissingKeys)
	}
}

func TestPreviewSet(t *testing.T) {
	h := newTestHome(t, fixture)
	diff, err := h.PreviewSet([]string{"model_providers", "1145"}, "base_url", "http://127.0.0.1:1800/v1")
	if err != nil {
		t.Fatal(err)
	}
	if diff.OldValue != "https://example.invalid/v1" {
		t.Errorf("OldValue = %q", diff.OldValue)
	}
	if diff.NewValue != "http://127.0.0.1:1800/v1" {
		t.Errorf("NewValue = %q", diff.NewValue)
	}
	// Preview 不得改动文件
	if got := readFile(t, h.ConfigPath()); !bytes.Equal(got, []byte(fixture)) {
		t.Error("Preview 改动了文件")
	}
}

func TestSnapshotAndClearProxyBak(t *testing.T) {
	h := newTestHome(t, fixture)
	if h.HasProxyBackup() {
		t.Fatal("初始不应有还原点")
	}
	if err := h.SnapshotProxyBak(); err != nil {
		t.Fatal(err)
	}
	if !h.HasProxyBackup() {
		t.Fatal("快照后应有还原点")
	}
	if err := h.ClearProxyBak(); err != nil {
		t.Fatal(err)
	}
	if h.HasProxyBackup() {
		t.Fatal("清理后不应有还原点")
	}
}
