package jsonwalk

import (
	"strings"
	"testing"
)

func TestParseKinds(t *testing.T) {
	cases := []struct {
		in   string
		want Kind
	}{
		{`{}`, KindObject},
		{`[]`, KindArray},
		{`""`, KindString},
		{`0`, KindNumber},
		{`-1.5e10`, KindNumber},
		{`true`, KindBool},
		{`false`, KindBool},
		{`null`, KindNull},
	}
	for _, c := range cases {
		v, err := Parse([]byte(c.in))
		if err != nil {
			t.Fatalf("Parse(%q) error: %v", c.in, err)
		}
		if v.Kind != c.want {
			t.Errorf("Parse(%q).Kind = %v, want %v", c.in, v.Kind, c.want)
		}
	}
}

func TestParseRejectsMalformed(t *testing.T) {
	bad := []string{
		``, `{`, `[`, `{"a"`, `{"a":}`, `{"a":1`, `[1,`, `[1 2]`,
		`"unterminated`, `"bad\escape"`, `"bad\u12"`, `01`, `--1`, `tru`,
		`{} extra`, `{,}`, `[1,]x`, "\"raw\x01ctl\"",
	}
	for _, s := range bad {
		if _, err := Parse([]byte(s)); err == nil {
			t.Errorf("Parse(%q) should have failed", s)
		}
	}
}

// 保序是核心要求：map 往返会打乱顺序，本包必须原样保留。
func TestPreservesKeyOrder(t *testing.T) {
	src := `{"z":1,"a":2,"m":3}`
	v, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	got := v.Members()
	want := []string{"z", "a", "m"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Members() = %v, want %v", got, want)
	}
}

// 大整数不得经过 float64。12345678901234567890 在 float64 下会失真。
func TestNumberKeepsLiteral(t *testing.T) {
	src := `{"n":12345678901234567890}`
	v, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	n, err := v.Member("n").Number()
	if err != nil {
		t.Fatal(err)
	}
	if n != "12345678901234567890" {
		t.Errorf("Number() = %q, want exact literal", n)
	}
}

func TestStringUnescape(t *testing.T) {
	cases := []struct{ in, want string }{
		{`"plain"`, "plain"},
		{`"a\nb"`, "a\nb"},
		{`"a\tb"`, "a\tb"},
		{`"q\"q"`, `q"q`},
		{`"back\\slash"`, `back\slash`},
		{`"\u0041"`, "A"},
		{`"\u4e2d\u6587"`, "中文"},
		// 代理对：U+1F600
		{`"\ud83d\ude00"`, "😀"},
	}
	for _, c := range cases {
		v, err := Parse([]byte(c.in))
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.in, err)
		}
		got, err := v.String()
		if err != nil {
			t.Fatalf("String(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("String(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// 重复 key 取最后一个，与 JSON 语义一致。
func TestDuplicateKeyTakesLast(t *testing.T) {
	v, err := Parse([]byte(`{"a":1,"a":2}`))
	if err != nil {
		t.Fatal(err)
	}
	n, _ := v.Member("a").Number()
	if n != "2" {
		t.Errorf("Member(a) = %q, want 2", n)
	}
}

// 转义形式的 key 应与未转义形式等价。
func TestEscapedKeyMatches(t *testing.T) {
	v, err := Parse([]byte(`{"\u0072ole":"user"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Member("role").MustString(); got != "user" {
		t.Errorf("escaped key lookup failed, got %q", got)
	}
}

func TestLookup(t *testing.T) {
	src := `{"input":[{"role":"user","content":[{"type":"input_text","text":"hi"}]}]}`
	v, _ := Parse([]byte(src))
	got := v.Lookup("input", 0, "content", 0, "text").MustString()
	if got != "hi" {
		t.Errorf("Lookup = %q, want hi", got)
	}
	if v.Lookup("input", 5) != nil {
		t.Error("out of range index should return nil")
	}
	if v.Lookup("nope") != nil {
		t.Error("missing key should return nil")
	}
}

// ★ 保真断言：改一个字段后，其余字节必须逐字节相同。
// 这是 PLAN.md §5.1 与 PHASES.md 阶段 0 的核心验收标准。
func TestReplaceStringPreservesRest(t *testing.T) {
	src := `{"model":"m","input": [ {"role":"user","content":[{"type":"input_text","text":"OLD"}]} ],"stream":false}`
	v, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	target := v.Lookup("input", 0, "content", 0, "text")
	if target == nil {
		t.Fatal("target not found")
	}

	out, err := ReplaceString([]byte(src), target, "NEW")
	if err != nil {
		t.Fatal(err)
	}

	// 除目标区间外，前后缀必须逐字节相同
	if got := string(out[:target.Start]); got != src[:target.Start] {
		t.Errorf("prefix changed:\n got %q\nwant %q", got, src[:target.Start])
	}
	if got := string(out[target.Start+len(`"NEW"`):]); got != src[target.End:] {
		t.Errorf("suffix changed:\n got %q\nwant %q", got, src[target.End:])
	}

	// 结果仍可解析，且新值正确
	v2, err := Parse(out)
	if err != nil {
		t.Fatalf("reparse failed: %v\nout=%s", err, out)
	}
	if got := v2.Lookup("input", 0, "content", 0, "text").MustString(); got != "NEW" {
		t.Errorf("value = %q, want NEW", got)
	}
}

// 替换含特殊字符的值，必须正确转义且不破坏结构。
func TestReplaceStringEscapes(t *testing.T) {
	src := `{"a":"x"}`
	v, _ := Parse([]byte(src))
	out, err := ReplaceString([]byte(src), v.Member("a"), "line1\nline2\t\"quoted\"\\back")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := Parse(out)
	if err != nil {
		t.Fatalf("reparse failed: %v\nout=%s", err, out)
	}
	want := "line1\nline2\t\"quoted\"\\back"
	if got := v2.Member("a").MustString(); got != want {
		t.Errorf("roundtrip = %q, want %q", got, want)
	}
}

func TestInsertFirst(t *testing.T) {
	t.Run("非空数组", func(t *testing.T) {
		src := `{"input":[{"a":1}]}`
		v, _ := Parse([]byte(src))
		arr := v.Member("input")
		out, err := InsertFirst([]byte(src), arr, []byte(`{"b":2}`))
		if err != nil {
			t.Fatal(err)
		}
		v2, err := Parse(out)
		if err != nil {
			t.Fatalf("reparse: %v\nout=%s", err, out)
		}
		a := v2.Member("input")
		if a.Len() != 2 {
			t.Fatalf("len = %d, want 2 (out=%s)", a.Len(), out)
		}
		if got := a.Index(0).Member("b").MustString(); got != "" {
			// Member("b") 是数字 2，MustString 返回空；用 Number 检查
			n, _ := a.Index(0).Member("b").Number()
			if n != "2" {
				t.Errorf("first element = %s, want {\"b\":2}", a.Index(0).Raw)
			}
		}
	})

	t.Run("空数组", func(t *testing.T) {
		src := `{"input":[]}`
		v, _ := Parse([]byte(src))
		out, err := InsertFirst([]byte(src), v.Member("input"), []byte(`{"b":2}`))
		if err != nil {
			t.Fatal(err)
		}
		v2, err := Parse(out)
		if err != nil {
			t.Fatalf("reparse: %v\nout=%s", err, out)
		}
		if v2.Member("input").Len() != 1 {
			t.Errorf("len = %d, want 1 (out=%s)", v2.Member("input").Len(), out)
		}
	})
}

func TestAppendLast(t *testing.T) {
	src := `{"input":[{"a":1}]}`
	v, _ := Parse([]byte(src))
	out, err := AppendLast([]byte(src), v.Member("input"), []byte(`{"b":2}`))
	if err != nil {
		t.Fatal(err)
	}
	v2, err := Parse(out)
	if err != nil {
		t.Fatalf("reparse: %v\nout=%s", err, out)
	}
	if v2.Member("input").Len() != 2 {
		t.Errorf("len = %d, want 2 (out=%s)", v2.Member("input").Len(), out)
	}
}

func TestDelete(t *testing.T) {
	cases := []string{
		`{"input":[{"a":1},{"b":2}]}`,
		`{"input":[{"a":1}]}`,
		`[{"a":1},{"b":2},{"c":3}]`,
	}
	for _, src := range cases {
		v, err := Parse([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		var arr *Value
		if v.Kind == KindArray {
			arr = v
		} else {
			arr = v.Member("input")
		}
		before := arr.Len()
		out, err := Delete([]byte(src), arr.Index(0))
		if err != nil {
			t.Fatalf("Delete(%s): %v", src, err)
		}
		v2, err := Parse(out)
		if err != nil {
			t.Fatalf("reparse after delete: %v\nsrc=%s\nout=%s", err, src, out)
		}
		var arr2 *Value
		if v2.Kind == KindArray {
			arr2 = v2
		} else {
			arr2 = v2.Member("input")
		}
		if arr2.Len() != before-1 {
			t.Errorf("Delete(%s): len %d -> %d, want %d (out=%s)", src, before, arr2.Len(), before-1, out)
		}
	}
}

func TestWalkVisitsAll(t *testing.T) {
	src := `{"a":{"b":1},"c":[1,2,{"d":3}]}`
	v, _ := Parse([]byte(src))
	count := 0
	v.Walk(func(*Value) bool { count++; return true })
	// 根 + a + b + c + 3 个元素 + d = 8
	if count != 8 {
		t.Errorf("Walk visited %d nodes, want 8", count)
	}
}

func FuzzParse(f *testing.F) {
	seeds := []string{
		`{}`, `[]`, `{"a":1}`, `{"a":[1,2,{"b":"c"}]}`,
		`{"input":[{"role":"user","content":[{"type":"input_text","text":"hi"}]}]}`,
		`"\ud83d\ude00"`, `{"n":12345678901234567890}`, `{"a":1,"a":2}`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		v, err := Parse(src)
		if err != nil {
			return // 非法输入返回 error 是预期行为
		}
		// 成功解析的必须能重新序列化出一致的结构
		if v.Kind == KindObject {
			for _, k := range v.Members() {
				if v.Member(k) == nil {
					t.Fatalf("Members() 列出 %q 但 Member 找不到", k)
				}
			}
		}
		// 区间必须落在 src 内
		v.Walk(func(n *Value) bool {
			if n.Start < 0 || n.End > len(src) || n.Start > n.End {
				t.Fatalf("bad range [%d,%d) for src len %d", n.Start, n.End, len(src))
			}
			return true
		})
	})
}
