package codexcfg

import (
	"bytes"
	"strings"
	"testing"
)

// fixture 复刻真实 ~/.codex/config.toml 的结构特征（阶段 3 实测）：
// 多个表头、带引号的表名、点分多层表头、顶层与表内同名键、行内数组、
// 含 Windows 路径的转义字符串。**不含任何真实凭据。**
//
// 刻意**不含**三个上下文默认键，以便测试注入路径（见 backup_test.go）。
const fixture = `model = "gpt-6-sol"
model_provider = "1145"

personality = "pragmatic"
model_reasoning_effort = "high"

sandbox_mode = "danger-full-access"
model_instructions_file = "./prompt-ctf-fusion.md"
notify = [ "C:\\tools\\codex-computer-use.exe", "turn-ended" ]

[model_providers.1145]
name = "hua"
wire_api = "responses"
base_url = "https://example.invalid/v1"
experimental_bearer_token = "sk-REDACTED"
requires_openai_auth = false

[desktop]
followUpQueueMode = "queue"
show-context-window-usage = true

[desktop.appearanceDarkChromeTheme.fonts]
code = "Cascadia Code"

[plugins."visualize@openai-bundled"]
enabled = true

[windows]
sandbox = "elevated"
`

func mustParse(t *testing.T, s string) *Doc {
	t.Helper()
	d, err := ParseDoc([]byte(s))
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	return d
}

// ★ 核心不变量 INV-6：未修改的文档必须逐字节一致。
func TestParseThenBytesIsIdentical(t *testing.T) {
	d := mustParse(t, fixture)
	if got := d.Bytes(); !bytes.Equal(got, []byte(fixture)) {
		t.Errorf("roundtrip changed bytes:\n got %d bytes\nwant %d bytes", len(got), len(fixture))
	}
}

func TestDetectEOL(t *testing.T) {
	if got := mustParse(t, "a = 1\nb = 2\n").EOL(); got != "\n" {
		t.Errorf("EOL = %q, want LF", got)
	}
	if got := mustParse(t, "a = 1\r\nb = 2\r\n").EOL(); got != "\r\n" {
		t.Errorf("EOL = %q, want CRLF", got)
	}
}

func TestTableParsingWithQuotesAndDots(t *testing.T) {
	d := mustParse(t, fixture)
	tables := d.Tables()

	want := [][]string{
		{"model_providers", "1145"},
		{"desktop"},
		{"desktop", "appearanceDarkChromeTheme", "fonts"},
		{"plugins", "visualize@openai-bundled"},
		{"windows"},
	}
	if len(tables) != len(want) {
		t.Fatalf("got %d tables, want %d: %v", len(tables), len(want), tables)
	}
	for i := range want {
		if strings.Join(tables[i], "\x00") != strings.Join(want[i], "\x00") {
			t.Errorf("table %d = %v, want %v", i, tables[i], want[i])
		}
	}
}

// 关键：顶层 name 与表内 name 必须区分开。
// 真实文件里 [model_providers.1145] 的 name = "hua"，顶层也可能有 name。
func TestScopeDistinguishesSameKeyName(t *testing.T) {
	src := `name = "top"

[model_providers.1145]
name = "hua"
`
	d := mustParse(t, src)

	if v, ok := d.GetString(nil, "name"); !ok || v != "top" {
		t.Errorf("顶层 name = %q (ok=%v), want top", v, ok)
	}
	if v, ok := d.GetString([]string{"model_providers", "1145"}, "name"); !ok || v != "hua" {
		t.Errorf("表内 name = %q (ok=%v), want hua", v, ok)
	}
}

func TestGetString(t *testing.T) {
	d := mustParse(t, fixture)

	cases := []struct {
		table []string
		key   string
		want  string
	}{
		{nil, "model", "gpt-6-sol"},
		{nil, "model_provider", "1145"},
		{nil, "model_instructions_file", "./prompt-ctf-fusion.md"},
		{[]string{"model_providers", "1145"}, "base_url", "https://example.invalid/v1"},
		{[]string{"model_providers", "1145"}, "wire_api", "responses"},
		{[]string{"desktop"}, "followUpQueueMode", "queue"},
		{[]string{"plugins", "visualize@openai-bundled"}, "enabled", "true"},
	}
	for _, c := range cases {
		got, ok := d.GetString(c.table, c.key)
		if !ok {
			t.Errorf("GetString(%v, %q) not found", c.table, c.key)
			continue
		}
		if got != c.want {
			t.Errorf("GetString(%v, %q) = %q, want %q", c.table, c.key, got, c.want)
		}
	}
}

// ★ 点修改的最小性：改一个键，其余字节必须完全相同。
func TestSetStringPreservesEverythingElse(t *testing.T) {
	d := mustParse(t, fixture)

	const oldURL = "https://example.invalid/v1"
	const newURL = "http://127.0.0.1:1800/v1"

	if err := d.SetString([]string{"model_providers", "1145"}, "base_url", newURL); err != nil {
		t.Fatalf("SetString: %v", err)
	}
	out := d.Bytes()

	// 只有旧值出现的位置被替换
	want := strings.Replace(fixture, oldURL, newURL, 1)
	if string(out) != want {
		t.Errorf("除目标值外还改了别的：\n got:\n%s\nwant:\n%s", out, want)
	}
	if !strings.Contains(string(out), shellPathLiteral) {
		t.Errorf("转义路径字面量被破坏")
	}
}

// notify 行含 Windows 路径与转义反斜杠，必须原样保留。
const shellPathLiteral = `C:\\tools\\codex-computer-use.exe`

func TestSetStringOnScalarKeepsInlineArray(t *testing.T) {
	d := mustParse(t, fixture)
	if err := d.SetString(nil, "model", "gpt-7"); err != nil {
		t.Fatal(err)
	}
	out := string(d.Bytes())
	if !strings.Contains(out, `notify = [ "C:\\tools\\codex-computer-use.exe", "turn-ended" ]`) {
		t.Errorf("数组行被改动:\n%s", out)
	}
	if !strings.Contains(out, `model = "gpt-7"`) {
		t.Errorf("目标键未更新:\n%s", out)
	}
}

func TestSetStringRejectsMissingKey(t *testing.T) {
	d := mustParse(t, fixture)
	err := d.SetString([]string{"model_providers", "1145"}, "nope", "x")
	if err == nil {
		t.Fatal("设置不存在的键应返回错误，而不是静默失败")
	}
}

func TestSetRaw(t *testing.T) {
	d := mustParse(t, `[model_providers.x]
base_url = "old"
`)
	if err := d.SetRaw([]string{"model_providers", "x"}, "base_url", []byte(`"new"`)); err != nil {
		t.Fatal(err)
	}
	if v, _ := d.GetString([]string{"model_providers", "x"}, "base_url"); v != "new" {
		t.Errorf("base_url = %q, want new", v)
	}
}

func TestInsertTopLevelIsIdempotent(t *testing.T) {
	d := mustParse(t, fixture)
	before := len(d.Bytes())
	// fixture 顶层已有 model，重复插入应无效果
	if !d.Has(nil, "model") {
		t.Fatal("fixture 应含顶层 model 键")
	}

	if err := d.InsertTopLevel("model", "other"); err != nil {
		t.Fatal(err)
	}
	if len(d.Bytes()) != before {
		t.Errorf("已存在的键不应重复插入")
	}
	if v, _ := d.GetString(nil, "model"); v != "gpt-6-sol" {
		t.Errorf("已存在的键不应被改写，got %q", v)
	}
}

func TestInsertTopLevelAddsMissingKey(t *testing.T) {
	d := mustParse(t, fixture)
	if err := d.InsertTopLevel("brand_new_key", "hello"); err != nil {
		t.Fatal(err)
	}
	out := d.Bytes()

	if !strings.Contains(string(out), `brand_new_key = "hello"`) {
		t.Errorf("新键未写入:\n%s", out)
	}
	// 必须插在顶层区域，不能落进某个表
	idx := bytes.Index(out, []byte("brand_new_key"))
	firstTable := bytes.Index(out, []byte("[model_providers.1145]"))
	if idx > firstTable {
		t.Errorf("新键被插到表内部（idx=%d, 首个表头=%d）", idx, firstTable)
	}
	// 重新解析后应能读到
	d2 := mustParse(t, string(out))
	if v, ok := d2.GetString(nil, "brand_new_key"); !ok || v != "hello" {
		t.Errorf("重解析后读不到新键: %q ok=%v", v, ok)
	}
}

func TestInsertTopLevelRaw(t *testing.T) {
	d := mustParse(t, "model = \"m\"\n")
	if err := d.InsertTopLevelRaw("count", []byte("42")); err != nil {
		t.Fatal(err)
	}
	out := d.Bytes()
	if !bytes.Contains(out, []byte("count = 42")) {
		t.Errorf("原始值插入失败:\n%s", out)
	}
}

func TestCRLFPreservedOnWrite(t *testing.T) {
	src := "model = \"a\"\r\n\r\n[model_providers.x]\r\nbase_url = \"old\"\r\n"
	d := mustParse(t, src)
	// SetString 接收**裸值**，引号由本包补（与 SetRaw 的区别就在此）
	if err := d.SetString([]string{"model_providers", "x"}, "base_url", "new"); err != nil {
		t.Fatal(err)
	}
	out := d.Bytes()
	if bytes.Contains(out, []byte("\n\n[model_providers")) && !bytes.Contains(out, []byte("\r\n\r\n[model_providers")) {
		t.Errorf("CRLF 行尾被破坏:\n%q", out)
	}
	if !bytes.Contains(out, []byte(`base_url = "new"`)) {
		t.Errorf("值未更新:\n%q", out)
	}
	// 插入也必须沿用 CRLF
	if err := d.InsertTopLevel("added", "v"); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(d.Bytes(), []byte("added = \"v\"\n")) && !bytes.Contains(d.Bytes(), []byte("added = \"v\"\r\n")) {
		t.Errorf("插入未沿用 CRLF:\n%q", d.Bytes())
	}
}

func TestInlineCommentPreserved(t *testing.T) {
	src := `model = "a" # 这是注释
`
	d := mustParse(t, src)
	if err := d.SetString(nil, "model", "b"); err != nil {
		t.Fatal(err)
	}
	out := string(d.Bytes())
	if !strings.Contains(out, "# 这是注释") {
		t.Errorf("行尾注释丢失:\n%s", out)
	}
	if !strings.Contains(out, `model = "b"`) {
		t.Errorf("值未更新:\n%s", out)
	}
}

// '#' 出现在字符串内时不是注释。
func TestHashInsideStringIsNotComment(t *testing.T) {
	src := `color = "#ff0000"
`
	d := mustParse(t, src)
	if v, ok := d.GetString(nil, "color"); !ok || v != "#ff0000" {
		t.Errorf("color = %q (ok=%v), want #ff0000", v, ok)
	}
}

func TestEscapedQuoteInValue(t *testing.T) {
	src := `msg = "he said \"hi\""
`
	d := mustParse(t, src)
	if v, ok := d.GetString(nil, "msg"); !ok || v != `he said "hi"` {
		t.Errorf("msg = %q (ok=%v)", v, ok)
	}
}

// 不支持的形态必须拒绝，而不是猜（ADR-001）。
func TestRejectsUnsupportedForms(t *testing.T) {
	bad := []struct{ name, src string }{
		{"array of tables", "[[products]]\nname = \"a\"\n"},
		{"multiline basic", "s = \"\"\"\nline\n\"\"\"\n"},
		{"multiline literal", "s = '''\nline\n'''\n"},
	}
	for _, c := range bad {
		if _, err := ParseDoc([]byte(c.src)); err == nil {
			t.Errorf("%s: 应拒绝解析", c.name)
		} else if !bytes.Contains([]byte(err.Error()), []byte("不支持")) {
			t.Errorf("%s: 错误信息应说明原因，got %v", c.name, err)
		}
	}
}

func TestQuoteTOMLString(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", `"plain"`},
		{`with "quote"`, `"with \"quote\""`},
		{`back\slash`, `"back\\slash"`},
		{"tab\there", `"tab\there"`},
		{"new\nline", `"new\nline"`},
	}
	for _, c := range cases {
		if got := quoteTOMLString(c.in); got != c.want {
			t.Errorf("quoteTOMLString(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestHasTable(t *testing.T) {
	d := mustParse(t, fixture)
	if !d.HasTable([]string{"model_providers", "1145"}) {
		t.Error("应找到 [model_providers.1145]")
	}
	if !d.HasTable([]string{"plugins", "visualize@openai-bundled"}) {
		t.Error("应找到带引号的表名")
	}
	if d.HasTable([]string{"nope"}) {
		t.Error("不应找到不存在的表")
	}
}
