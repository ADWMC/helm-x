package codexcfg

import (
	"bytes"
	"testing"
)

// 本文件守的是一个已修复缺陷的回归（2026-10 audit）：
//
// 旧实现把多行值（数组 / 行内表跨行）的续行当成 LineOther，
// 于是 `notify = [` 之后的 `  "a",` 触发
// `ErrUnsupportedForm: 无法解析的行 "\"a\","`，
// 整份 config.toml 解析失败 → apply / 自检 / 守护全线失败。
// 而这些写法在 TOML 里完全合法，codex 自己的默认配置就可能这么写。
//
// 现在的契约（见 doc.go ParseDoc / valueBalanced / stripInlineComment）：
//   - 键值行的值未闭合时，继续吸收后续行，直到闭合；
//   - 被吸收的整段只占一个 Line，Raw 覆盖整段（含各行换行），
//     ValueRange 只覆盖值本身（含整段，不含行尾注释）；
//   - 多行**字符串**（""" / '''）与 [[array of tables]] 仍然拒绝（ADR-001）。

// multilineArraySrc 是最小复现：顶层多行数组 + 之后还有表头和键。
// 关键是数组之后仍有内容 —— 吸收多行时越界/丢字节会立刻暴露。
const multilineArraySrc = `model_provider = "r"
notify = [
  "a",
  "b",
]

[model_providers.r]
base_url = "https://up.example/v1"
`

// 守：ParseDoc 能吸收多行数组，且吸收之后文档结构仍然正确可读。
// 旧实现在这里直接返回 ErrUnsupportedForm，后面的表与键根本取不到。
func TestParseMultilineArray(t *testing.T) {
	d, err := ParseDoc([]byte(multilineArraySrc))
	if err != nil {
		t.Fatalf("ParseDoc 应接受多行数组，实际报错: %v", err)
	}

	// 数组之前的键仍可读。
	if got, ok := d.GetString(nil, "model_provider"); !ok || got != "r" {
		t.Errorf("model_provider = %q (ok=%v), want %q", got, ok, "r")
	}

	// 数组本身的区间覆盖整段值（不含换行结尾的 `]` 之外的内容）。
	if got, ok := d.Get(nil, "notify"); !ok {
		t.Errorf("notify 键应存在 (ok=%v)", ok)
	} else if want := "[\n  \"a\",\n  \"b\",\n]"; string(got) != want {
		t.Errorf("notify 值区间 = %q, want %q（ValueRange 应覆盖整段多行值）", string(got), want)
	}

	// 吸收多行之后，后续表头必须仍然被正确识别（Probe 依赖 HasTable）。
	if !d.HasTable([]string{"model_providers", "r"}) {
		t.Errorf("应找到 [model_providers.r]，实际表列表 = %v", d.Tables())
	}
	if got, ok := d.GetString([]string{"model_providers", "r"}, "base_url"); !ok ||
		got != "https://up.example/v1" {
		t.Errorf("[model_providers.r].base_url = %q (ok=%v), want %q",
			got, ok, "https://up.example/v1")
	}
}

// 守 INV-6：解析未修改的文档后 Bytes() 必须与输入逐字节相同。
// 多行吸收是唯一会把多行拼进一条 Line.Raw 的路径，最容易在这里漏/重字节。
func TestMultilineArrayPreservesOtherBytes(t *testing.T) {
	d, err := ParseDoc([]byte(multilineArraySrc))
	if err != nil {
		t.Fatalf("ParseDoc 应接受多行数组，实际报错: %v", err)
	}
	got := d.Bytes()
	if !bytes.Equal(got, []byte(multilineArraySrc)) {
		t.Errorf("往返后字节不一致: got %d bytes, want %d bytes\n got=%q\nwant=%q",
			len(got), len(multilineArraySrc), string(got), multilineArraySrc)
	}
}

// 守：多行值的 Line.Raw 覆盖整段，所以改「别的」键时不会误伤它；
// 反过来，多行段内的字节也不会被当成本行的值边界而截断。
// 这里改 [model_providers.r].base_url 这个普通标量，断言多行数组完好无损。
func TestSetStringOnMultilineValueRewritesWholeSegment(t *testing.T) {
	const newURL = "http://127.0.0.1:1800/v1"

	d, err := ParseDoc([]byte(multilineArraySrc))
	if err != nil {
		t.Fatalf("ParseDoc 应接受多行数组，实际报错: %v", err)
	}

	if err := d.SetString([]string{"model_providers", "r"}, "base_url", newURL); err != nil {
		t.Fatalf("SetString base_url 失败: %v", err)
	}

	// 1) base_url 已改。
	if got, ok := d.GetString([]string{"model_providers", "r"}, "base_url"); !ok || got != newURL {
		t.Errorf("[model_providers.r].base_url = %q (ok=%v), want %q", got, ok, newURL)
	}

	// 2) 多行数组整段仍在，且字节形状没变。
	const arrayBlock = "notify = [\n  \"a\",\n  \"b\",\n]\n"
	if !bytes.Contains(d.Bytes(), []byte(arrayBlock)) {
		t.Errorf("改动其他键后多行数组被破坏，产物中没有 %q\n实际产物:\n%s",
			arrayBlock, d.Bytes())
	}
	if got, ok := d.Get(nil, "notify"); !ok || string(got) != "[\n  \"a\",\n  \"b\",\n]" {
		t.Errorf("改动其他键后 notify 值区间 = %q (ok=%v), want %q",
			string(got), ok, "[\n  \"a\",\n  \"b\",\n]")
	}

	// 3) 逐字节：除 base_url 那一行的值之外完全不变。
	want := "model_provider = \"r\"\n" +
		arrayBlock +
		"\n[model_providers.r]\n" +
		"base_url = \"" + newURL + "\"\n"
	if got := string(d.Bytes()); got != want {
		t.Errorf("产物与预期不一致:\n got=%q\nwant=%q", got, want)
	}
}

// 守 stripInlineComment：多行值内部（depth>0）的 '#' 是数组注释，不是行尾注释。
// 旧/朴素实现会在 '#' 处切掉值 → 值变成 `[\n  "a",`，闭合判定失败，
// 于是把后面的 `"b",` 当续行 → 又一次 ErrUnsupportedForm。
func TestMultilineArrayWithInnerComment(t *testing.T) {
	const src = `model_provider = "r"
notify = [
  "a", # 第一项
  "b",
]

[model_providers.r]
base_url = "https://up.example/v1"
`

	d, err := ParseDoc([]byte(src))
	if err != nil {
		t.Fatalf("多行数组内含注释应被接受，实际报错: %v", err)
	}

	if got, ok := d.GetString([]string{"model_providers", "r"}, "base_url"); !ok ||
		got != "https://up.example/v1" {
		t.Errorf("[model_providers.r].base_url = %q (ok=%v), want %q",
			got, ok, "https://up.example/v1")
	}

	// '#' 之后的字节不会被当注释丢弃：它们留在值区间里。
	// 这是当前实现的既定行为（depth>0 时 '#' 不结束值），在此固化，
	// 将来若改为"剥离数组内注释"，这里会明确地失败而不是静默变化。
	if got, ok := d.Get(nil, "notify"); !ok {
		t.Errorf("notify 键应存在 (ok=%v)", ok)
	} else if !bytes.Contains(got, []byte("# 第一项")) {
		t.Errorf("notify 值区间应保留数组内注释文本，实际 = %q", string(got))
	}

	// 注释行同样不影响往返。
	if got := d.Bytes(); !bytes.Equal(got, []byte(src)) {
		t.Errorf("往返后字节不一致: got=%q\nwant=%q", string(got), src)
	}
}

// 守 ADR-001 的另一半：放宽多行**值**不等于放宽多行字符串与 array of tables。
// 这两类仍然是"看不懂就拒绝"，绝不能因为吸收逻辑而漏过。
func TestMultilineStillRejectsUnsupported(t *testing.T) {
	bad := []struct{ name, src string }{
		{"multiline basic string", "s = \"\"\"\nline\n\"\"\"\n"},
		{"multiline literal string", "s = '''\nline\n'''\n"},
		{"array of tables", "[[products]]\nname = \"a\"\n"},
		{"array of tables after multiline array", "notify = [\n  \"a\",\n]\n[[products]]\n"},
	}
	for _, c := range bad {
		d, err := ParseDoc([]byte(c.src))
		if err == nil {
			t.Fatalf("%s: 应拒绝解析，实际成功且 Bytes()=%q", c.name, string(d.Bytes()))
		}
		if !bytes.Contains([]byte(err.Error()), []byte("不支持")) {
			t.Errorf("%s: 错误信息应含「不支持」，实际 err=%v", c.name, err)
		}
		if !bytes.Contains([]byte(err.Error()), []byte(ErrUnsupportedForm.Error())) {
			t.Errorf("%s: 错误应包装 ErrUnsupportedForm，实际 err=%v", c.name, err)
		}
	}
}
