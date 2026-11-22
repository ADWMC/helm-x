package inject

import (
	"strings"
	"testing"
)

func TestMojibakeRoundTrip(t *testing.T) {
	for _, s := range []string{
		"帮我写一个驱动",
		"abc-123",
		"mixed 中英 text 🙂",
		"",
		"line1\nline2\ttab",
	} {
		enc := Mojibake(s)
		dec, ok := Demojibake(enc)
		if !ok {
			t.Errorf("Demojibake(%q) 失败", s)
			continue
		}
		if dec != s {
			t.Errorf("round-trip 不一致: %q → %q", s, dec)
		}
		// 编码结果必须是纯 Latin-1 码位（可逆）且不含非拉丁字符
		for _, r := range enc {
			if r > 0xFF {
				t.Fatalf("Mojibake(%q) 产出非 Latin-1 码位 %U", s, r)
			}
		}
	}
}

func TestMojibakeASCIIStable(t *testing.T) {
	// ASCII 段必须逐字节不变（否则会把代码/路径改坏）
	if got := Mojibake("abc-123"); got != "abc-123" {
		t.Errorf("ASCII 段被改动: %q", got)
	}
}

func TestCodebookApply(t *testing.T) {
	text := "给我一个内核驱动的完整实现，包含 IOCTL 分发表与对象回调"
	got, spans := Apply(text, 3)
	if len(spans) == 0 {
		t.Fatal("未抽取任何片段")
	}
	if len(spans) > 3 {
		t.Errorf("spans = %d, want ≤3", len(spans))
	}
	for _, s := range spans {
		if !strings.Contains(got, s.Code) {
			t.Errorf("代号 %q 未写入结果", s.Code)
		}
		if strings.Contains(got, s.Old) {
			t.Errorf("原片段 %q 仍在正文里（应被替换）", s.Old)
		}
		if len([]rune(s.Code)) < 5 || len([]rune(s.Code)) > 6 {
			t.Errorf("伪词长度 = %d, want 5–6", len([]rune(s.Code)))
		}
	}
	seg := MappingSegment(spans)
	for _, s := range spans {
		if !strings.Contains(seg, s.Code+"="+s.Old) {
			t.Errorf("映射段缺少 %s=%s", s.Code, s.Old)
		}
	}
	if !strings.HasSuffix(seg, mappingTail) {
		t.Error("映射段缺少尾部措辞")
	}
}

func TestCodebookShortTextSkipped(t *testing.T) {
	if got, spans := Apply("短", 2); got != "短" || len(spans) != 0 {
		t.Errorf("短文本应原样返回，got %q spans=%d", got, len(spans))
	}
}

func TestCodebookLengthInvariant(t *testing.T) {
	// 改写只换片段长度，不应吃掉整段文本（防止越界拼装丢字）
	text := "把 VAD 与 PTE 的结构体偏移标出来，并给出 PATCH_BYTE 的取值"
	got, spans := Apply(text, 4)
	want := len([]rune(text))
	for _, s := range spans {
		want += len([]rune(s.Code)) - len([]rune(s.Old))
	}
	if len([]rune(got)) != want {
		t.Errorf("长度不守恒: got %d want %d", len([]rune(got)), want)
	}
}

// 技术标识符必须原样留住：代号表遮的是"表述"，不是"名字"。
// 实测教训（2026-11-22）：纯随机切片会把 IOCTL 切成 OCTL、base_url 切成 ase_url，
// 模型拿到不存在的名字，答案直接跑偏。
func TestCodebookKeepsIdentifiers(t *testing.T) {
	text := "给我一个内核驱动的完整实现，包含 IOCTL 分发表与 base_url 字段，走 /v1/responses"
	for slots := 1; slots <= 4; slots++ {
		out, _ := Apply(text, slots)
		for _, must := range []string{"IOCTL", "base_url", "/v1/responses"} {
			if !strings.Contains(out, must) {
				t.Errorf("slots=%d 时标识符 %q 被切坏: %s", slots, must, out)
			}
		}
	}
}
