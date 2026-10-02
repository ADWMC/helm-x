package codexcfg

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// 用**真实 config.toml 的副本**做往返测试，而不是我构造的 fixture。
//
// 理由：fixture 是"我以为的真实结构"；阶段 A 的教训是别用推断代替观察。
// 本测试从 $HELMX_REAL_CONFIG 指定的路径读取真实文件副本；
// 未设置时跳过（CI 上不存在该文件）。
//
// 真实文件含凭据，因此：
//   - 只读副本，测试结束即随 t.TempDir() 清理
//   - 不写入仓库、不打印内容（仅打印长度与行数）
func TestRoundTripAgainstRealConfig(t *testing.T) {
	src := os.Getenv("HELMX_REAL_CONFIG")
	if src == "" {
		t.Skip("未设置 HELMX_REAL_CONFIG，跳过真实文件往返测试")
	}
	origin, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("读取 %s: %v", src, err)
	}

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ConfigName)
	if err := os.WriteFile(cfgPath, origin, 0o644); err != nil {
		t.Fatal(err)
	}
	h := Home{Dir: dir}

	t.Logf("真实配置: %d 字节, %d 行", len(origin), bytes.Count(origin, []byte("\n")))

	// 1) 纯解析往返：不应改动任何字节
	d, err := ParseDoc(origin)
	if err != nil {
		t.Fatalf("真实配置解析失败: %v", err)
	}
	if got := d.Bytes(); !bytes.Equal(got, origin) {
		t.Fatalf("解析往返改动了字节 (%d -> %d)", len(origin), len(got))
	}
	t.Logf("表数量: %d", len(d.Tables()))

	// 2) 探测激活 provider
	ap, err := h.Probe()
	if err != nil {
		t.Fatalf("Probe 失败: %v", err)
	}
	t.Logf("激活 provider: %s, wire_api=%q, 本地=%v", ap.Name, ap.WireAPI, ap.IsLocalProxy())

	// 3) 完整 apply -> remove 往返
	relayBefore, _ := h.RelayURL()

	// 切到代理态前先存还原点（由 Home 层负责，不在 Doc 里）
	if !ap.IsLocalProxy() {
		if err := h.SnapshotProxyBak(); err != nil {
			t.Fatalf("存还原点失败: %v", err)
		}
	}

	err = h.Update(func(doc *Doc) error {
		if err := doc.SetString(ap.Table, "base_url", "http://127.0.0.1:1800/v1"); err != nil {
			return err
		}
		for _, kv := range ContextDefaultKeys {
			if err := doc.InsertTopLevelRaw(kv.Key, []byte(kv.Val)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("apply 失败: %v", err)
	}

	// 4) 验证注入确实生效
	st := h.CheckInjection()
	if !st.Injected {
		t.Errorf("apply 后应报告已注入，MissingKeys=%v", st.MissingKeys)
	}

	// 5) 还原并逐字节比对
	if err := h.RestoreFromBackup(); err != nil {
		t.Fatalf("还原失败: %v", err)
	}
	got, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, origin) {
		// 找出第一处差异，便于定位
		n := len(origin)
		if len(got) < n {
			n = len(got)
		}
		at := -1
		for i := 0; i < n; i++ {
			if got[i] != origin[i] {
				at = i
				break
			}
		}
		t.Fatalf("★ INV-6 失败：还原后不是逐字节一致。原 %d 字节，还原后 %d 字节，首处差异 @%d",
			len(origin), len(got), at)
	}

	relayAfter, _ := h.RelayURL()
	if relayBefore != relayAfter {
		t.Errorf("往返后 relay 地址变化: %q -> %q", relayBefore, relayAfter)
	}
	t.Log("INV-6 通过：逐字节一致")
}
