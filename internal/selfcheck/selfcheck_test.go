package selfcheck

import "testing"

// TestRunEmbeddedAssets 断言第 5 项「内嵌资源」在仓库正常状态下通过。
//
// 为什么用"找到才断言"而不是直接取 r.Checks[4]：
// Run 第 1 项读真实 ~/.codex，目录不存在时它会 FAIL 并把后续 5 项替换成
// 「（依赖前一项）」的跳过占位 —— 那种情况下第 5 项根本不存在，
// 断言下标会得到一个和本改动无关的失败。这里只在拿到真实报告时校验，否则 Skip。
func TestRunEmbeddedAssets(t *testing.T) {
	r := Run(false, nil)

	var got *Check
	for i := range r.Checks {
		if r.Checks[i].Name == "内嵌资源" {
			got = &r.Checks[i]
			break
		}
	}
	if got == nil {
		t.Skip("codex 配置目录缺失，后续检查被整体跳过；本机无 ~/.codex，跳过资源项断言")
	}

	// 检查项总数口径不能变：CLI/GUI 的进度分母依赖它。
	if len(r.Checks) != 6 {
		t.Fatalf("检查项数量 = %d, want 6（e2e=false 时不应增删检查项）", len(r.Checks))
	}
	// 第 5 项的位置也不能变。
	if idx := indexOfName(r.Checks, "内嵌资源"); idx != 4 {
		t.Fatalf("「内嵌资源」在下标 %d, want 4（第 5 项）", idx)
	}
	if got.Skip {
		t.Fatalf("「内嵌资源」被跳过，detail=%q", got.Detail)
	}
	if !got.OK {
		t.Fatalf("「内嵌资源」未通过，detail=%q", got.Detail)
	}
	// -v 下把 Detail 打出来，人工核对格式是否可定位故障（模式数/规则字节/密钥来源）。
	t.Logf("第 5 项 detail = %q", got.Detail)
}

func indexOfName(cs []Check, name string) int {
	for i := range cs {
		if cs[i].Name == name {
			return i
		}
	}
	return -1
}
