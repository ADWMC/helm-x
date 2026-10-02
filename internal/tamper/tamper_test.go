package tamper

import (
	"strings"
	"testing"
)

const rulesFixture = `# 注释行
(?:不能|无法|不可以).{0,10}(?:协助|帮助|提供)
I(?:'m| am)? sorry.{0,30}(?:can(?:'t|not)|unable)
flagged for possible cybersecurity
网络安全策略

# 末尾注释
`

func TestNew(t *testing.T) {
	e, errs := New(rulesFixture)
	if len(errs) != 0 {
		t.Fatalf("不应有编译错误: %v", errs)
	}
	if e.Len() != 4 {
		t.Errorf("加载 %d 条规则，want 4（注释与空行应被跳过）", e.Len())
	}
}

// 非法正则应被跳过而不是让整个引擎失效。
func TestNewSkipsInvalidRegex(t *testing.T) {
	e, errs := New("无法协助\n([unclosed\n不能提供\n")
	if len(errs) != 1 {
		t.Errorf("应报告 1 个错误，得到 %d", len(errs))
	}
	if e.Len() != 2 {
		t.Errorf("有效规则应为 2 条，得到 %d", e.Len())
	}
	// 引擎仍可用
	if !e.IsRefusal("我无法协助") {
		t.Error("跳过非法规则后引擎应仍工作")
	}
}

func TestIsRefusal(t *testing.T) {
	e, _ := New(rulesFixture)

	cases := []struct {
		text string
		want bool
	}{
		{"抱歉，我无法协助这个请求。", true},
		{"我不能提供该内容。", true},
		{"I'm sorry, I can't help with that.", true},
		{"flagged for possible cybersecurity", true},
		{"该会话已被网络安全策略屏蔽", true},
		{"这是正常的输出内容。", false},
		{"", false},
		{"func main() { fmt.Println(\"hello\") }", false},
	}
	for _, c := range cases {
		if got := e.IsRefusal(c.text); got != c.want {
			t.Errorf("IsRefusal(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestMatchReturnsSource(t *testing.T) {
	e, _ := New(rulesFixture)
	src := e.Match("我无法协助")
	if src == "" {
		t.Fatal("应命中一条规则")
	}
	if !strings.Contains(src, "协助") {
		t.Errorf("返回的源文本不对: %q", src)
	}
	if got := e.Match("正常内容"); got != "" {
		t.Errorf("未命中应返回空串，得到 %q", got)
	}
}

// ★ INV-8：Attach 只在前面加标记，绝不删除原文。
func TestAttachPreservesOriginal(t *testing.T) {
	e, _ := New(rulesFixture)
	const original = "抱歉，我无法协助这个请求。"

	out := e.Attach(original)
	if !strings.Contains(out, original) {
		t.Errorf("★ INV-8 违反：原文被破坏\n got: %q\nwant 包含: %q", out, original)
	}
	if !strings.Contains(out, "Rei Protocol") {
		t.Errorf("应包含标记，得到 %q", out)
	}
	if !strings.HasPrefix(out, e.Marker()) {
		t.Error("标记应在最前面")
	}
}

// Attach 幂等：重复调用不叠加标记。
func TestAttachIdempotent(t *testing.T) {
	e, _ := New(rulesFixture)
	once := e.Attach("原文")
	twice := e.Attach(once)
	if once != twice {
		t.Errorf("重复 Attach 不应叠加\n once: %q\ntwice: %q", once, twice)
	}
}

func TestAttachEmptyText(t *testing.T) {
	e, _ := New(rulesFixture)
	out := e.Attach("")
	if !strings.Contains(out, "Rei Protocol") {
		t.Errorf("空文本也应产出标记，得到 %q", out)
	}
}

func TestWithMarker(t *testing.T) {
	e, _ := New(rulesFixture)
	e2 := e.WithMarker("[CUSTOM]\n")
	if e2.Marker() != "[CUSTOM]\n" {
		t.Errorf("Marker = %q", e2.Marker())
	}
	// 原引擎不受影响
	if strings.Contains(e.Marker(), "CUSTOM") {
		t.Error("WithMarker 不应修改原引擎")
	}
	if out := e2.Attach("x"); !strings.HasPrefix(out, "[CUSTOM]\n") {
		t.Errorf("自定义标记未生效: %q", out)
	}
}

func TestRulesList(t *testing.T) {
	e, _ := New(rulesFixture)
	rules := e.Rules()
	if len(rules) != 4 {
		t.Errorf("Rules() 返回 %d 条，want 4", len(rules))
	}
	for _, r := range rules {
		if strings.HasPrefix(r, "#") || strings.TrimSpace(r) == "" {
			t.Errorf("Rules() 不应包含注释或空行: %q", r)
		}
	}
}

// nil 引擎不应 panic（调用方可能没配规则）。
func TestNilEngineSafe(t *testing.T) {
	var e *Engine
	if e.IsRefusal("无法协助") {
		t.Error("nil 引擎应返回 false")
	}
	if e.Len() != 0 {
		t.Error("nil 引擎长度应为 0")
	}
	if e.Match("x") != "" {
		t.Error("nil 引擎 Match 应返回空串")
	}
	if out := e.Attach("原文"); !strings.Contains(out, "原文") {
		t.Errorf("nil 引擎 Attach 应至少保留原文，得到 %q", out)
	}
}
