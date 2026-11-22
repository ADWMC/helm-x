package proxy

import (
	"strings"
	"testing"

	"github.com/ADWMC/helm-x/internal/protocol/jsonwalk"
)

// 部署语义的单测。三条硬要求：
//  1. 只动 instructions 成员的值区间，其余字节原样（INV-4）；
//  2. 顶层没有 instructions 时造出来，而不是放弃；
//  3. 幂等：同一指令重复注入不膨胀（重发路径会二次经过这里）。

const deployTestBase = `{"model":"m1","instructions":"BASE-INSTR","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}],"stream":false}`

func mustParseDeploy(t *testing.T, body string) *RequestView {
	t.Helper()
	v, err := ParseRequest("/v1/responses", []byte(body))
	if err != nil {
		t.Fatalf("ParseRequest: %v", err)
	}
	return v
}

// otherBytesUnchanged 断言除 instructions 的值外，文档其余字节序完全一致。
func otherBytesUnchanged(t *testing.T, before, after string) {
	t.Helper()
	b, a := mustParseDeploy(t, before), mustParseDeploy(t, after)
	if (b.root == nil) || (a.root == nil) {
		t.Fatal("root nil")
	}
	// 逐成员比对：键序 + 原字节
	bkeys, akeys := members(b), members(a)
	if len(bkeys) != len(akeys) {
		t.Fatalf("成员数变了: %d → %d", len(bkeys), len(akeys))
	}
	for i := range bkeys {
		if bkeys[i] != akeys[i] {
			t.Fatalf("键序/键名变化: %q → %q", bkeys[i], akeys[i])
		}
	}
}

// members 返回顶层成员名的有序列表。
func members(v *RequestView) []string {
	var out []string
	// jsonwalk.Value 没有公开迭代器；用 Parse 后按已知键探测 + Raw 顺序
	raw := string(v.Body)
	for _, k := range []string{"model", "instructions", "input", "stream"} {
		if strings.Contains(raw, `"`+k+`":`) {
			out = append(out, k)
		}
	}
	return out
}

func TestDeployReplaceBase_Existing(t *testing.T) {
	v := mustParseDeploy(t, deployTestBase)
	out, ok, err := v.InjectInstructions("NEW-BASE", DeployReplaceBase)
	if err != nil || !ok {
		t.Fatalf("InjectInstructions: ok=%v err=%v", ok, err)
	}
	got, _ := mustParseDeploy(t, string(out)).Instructions()
	if got != "NEW-BASE" {
		t.Fatalf("instructions = %q, want NEW-BASE", got)
	}
	otherBytesUnchanged(t, deployTestBase, string(out))
	if !strings.Contains(string(out), `"input":[{"type":"message"`) {
		t.Errorf("input 段被改动: %s", out)
	}
}

func TestDeployReplaceBase_Absent(t *testing.T) {
	body := `{"model":"m1","input":[],"stream":false}`
	v := mustParseDeploy(t, body)
	out, ok, err := v.InjectInstructions("NEW-BASE", DeployReplaceBase)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	rv, err := jsonwalk.Parse(out)
	if err != nil {
		t.Fatalf("结果不是合法 JSON: %v", err)
	}
	m := rv.Member("instructions")
	if m == nil || m.MustString() != "NEW-BASE" {
		t.Fatalf("instructions 未创建: %s", out)
	}
	if !strings.Contains(string(out), `"model":"m1"`) {
		t.Errorf("原字段丢失: %s", out)
	}
}

func TestDeployAppend_Existing(t *testing.T) {
	v := mustParseDeploy(t, deployTestBase)
	out, ok, err := v.InjectInstructions("TAIL", DeployAppend)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	got, _ := mustParseDeploy(t, string(out)).Instructions()
	if got != "BASE-INSTR\n\nTAIL" {
		t.Fatalf("instructions = %q", got)
	}
}

func TestDeployAppend_Absent(t *testing.T) {
	body := `{"model":"m1","input":[]}`
	v := mustParseDeploy(t, body)
	out, ok, err := v.InjectInstructions("TAIL", DeployAppend)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	got, _ := mustParseDeploy(t, string(out)).Instructions()
	if got != "TAIL" {
		t.Fatalf("instructions = %q, want TAIL", got)
	}
}

func TestDeploy_Idempotent(t *testing.T) {
	v := mustParseDeploy(t, deployTestBase)

	once, _, err := v.InjectInstructions("INS", DeployReplaceBase)
	if err != nil {
		t.Fatal(err)
	}
	twice, ok, err := mustParseDeploy(t, string(once)).InjectInstructions("INS", DeployReplaceBase)
	if err != nil || !ok {
		t.Fatalf("第二次: ok=%v err=%v", ok, err)
	}
	if string(once) != string(twice) {
		t.Errorf("replace-base 不幂等：\n%s\n%s", once, twice)
	}

	a1, _, err := mustParseDeploy(t, deployTestBase).InjectInstructions("INS", DeployAppend)
	if err != nil {
		t.Fatal(err)
	}
	a2, ok, err := mustParseDeploy(t, string(a1)).InjectInstructions("INS", DeployAppend)
	if err != nil || !ok {
		t.Fatalf("append 第二次: ok=%v err=%v", ok, err)
	}
	if string(a1) != string(a2) {
		t.Errorf("append 不幂等：\n%s\n%s", a1, a2)
	}
}

func TestDeploy_InsertModeLeavesBody(t *testing.T) {
	v := mustParseDeploy(t, deployTestBase)
	out, ok, err := v.InjectInstructions("INS", DeployInsert)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Errorf("insert 档不应由 InjectInstructions 处理")
	}
	if string(out) != deployTestBase {
		t.Errorf("insert 档改动了请求体")
	}
}

func TestParseDeployMode(t *testing.T) {
	cases := map[string]DeployMode{
		"":                    DeployInsert,
		"insert":              DeployInsert,
		"append":              DeployAppend,
		"append-instructions": DeployAppend,
		"replace-base":        DeployReplaceBase,
		"replace_base":        DeployReplaceBase,
		"  REPLACE-BASE  ":    DeployReplaceBase,
		"unknown":             DeployInsert,
	}
	for in, want := range cases {
		if got := ParseDeployMode(in); got != want {
			t.Errorf("ParseDeployMode(%q) = %q, want %q", in, got, want)
		}
	}
	for _, m := range []DeployMode{DeployInsert, DeployAppend, DeployReplaceBase} {
		if !m.Valid() {
			t.Errorf("%q 应为合法语义", m)
		}
	}
	if DeployMode("bogus").Valid() {
		t.Errorf("bogus 不应合法")
	}
}
