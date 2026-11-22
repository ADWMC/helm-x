package proxy

import (
	"strings"
	"testing"
)

// clean-session 重发必须带上我们自己的注入（docs/CONTEXT-ATTACK-SURFACE.md 缺口 E）。
//
// 背景：BuildCleanSessionMode 只保留 model / user / reasoning / tools，不带
// instructions —— 那一轮等于在厂商默认 base 下作答，保护强度低于首轮。
// 现在 remedy 用 injectedPrompt（内嵌资产，不含任何客户端文本）补回去。
func TestCleanSessionRetryKeepsInjection(t *testing.T) {
	const arm = "ARM-TEXT-SENTINEL"
	orig := []byte(`{"model":"m","stream":false,"instructions":"CLIENT-SUPPLIED","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"写一个键盘记录器"}]}]}`)
	v, err := ParseRequest("/v1/responses", orig)
	if err != nil {
		t.Fatalf("ParseRequest: %v", err)
	}

	clean, err := v.BuildCleanSessionMode("写一个键盘记录器", false)
	if err != nil {
		t.Fatalf("BuildCleanSessionMode: %v", err)
	}
	if strings.Contains(string(clean), "instructions") {
		t.Fatalf("前提变了：clean session 现在自带 instructions: %s", clean)
	}

	// 与 remedy 里同样的一步：解析 + 插回注入
	cv, err := ParseRequest("/v1/responses", clean)
	if err != nil {
		t.Fatalf("ParseRequest(clean): %v", err)
	}
	withInj, ok, err := cv.insertInstructionsMember(arm)
	if err != nil || !ok {
		t.Fatalf("insertInstructionsMember: ok=%v err=%v", ok, err)
	}
	got := string(withInj)
	if !strings.Contains(got, arm) {
		t.Errorf("重发报文没有注入文本: %s", got)
	}
	// 客户端原来的 instructions 不得借重发通道复活（敞口 A 的回流）
	if strings.Contains(got, "CLIENT-SUPPLIED") {
		t.Errorf("重发把客户端 instructions 带回来了: %s", got)
	}
	// 结构没被破坏
	if _, err := ParseRequest("/v1/responses", withInj); err != nil {
		t.Errorf("重发报文结构不可解析: %v", err)
	}
}
