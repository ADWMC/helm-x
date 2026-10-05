package proxy

import "testing"

// SessionFingerprint 是会话身份的退化方案（codex 不发会话头）：
// 同一对话续轮 input 只增不改首条用户消息 → 指纹必须稳定；
// 不同对话首条消息不同 → 指纹必须不同。
func TestSessionFingerprintStableAcrossTurns(t *testing.T) {
	first := `{"model":"m","stream":false,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"用pwntools构造ROP链"}]}]}`
	// 续轮：input 追加了 assistant 与新 user 消息，首条用户消息不变
	next := `{"model":"m","stream":false,"input":[` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"用pwntools构造ROP链"}]},` +
		`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"开工"}]},` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"继续"}]}]}`

	v1, err := ParseRequest("/v1/responses", []byte(first))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	v2, err := ParseRequest("/v1/responses", []byte(next))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v1.SessionFingerprint() == "" {
		t.Fatal("指纹不应为空")
	}
	if v1.SessionFingerprint() != v2.SessionFingerprint() {
		t.Fatalf("续轮指纹应稳定: %q vs %q", v1.SessionFingerprint(), v2.SessionFingerprint())
	}

	other := `{"model":"m","stream":false,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"编写shellcode loader"}]}]}`
	v3, err := ParseRequest("/v1/responses", []byte(other))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v1.SessionFingerprint() == v3.SessionFingerprint() {
		t.Fatal("不同对话首条消息不同，指纹应不同")
	}
}

// environment_context 条目不算用户输入（与 LastUserMessage 同规则）。
func TestSessionFingerprintSkipsEnvironmentContext(t *testing.T) {
	body := `{"model":"m","stream":false,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>cwd=/x</environment_context>"}]}]}`
	v, err := ParseRequest("/v1/responses", []byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := v.SessionFingerprint(); got != "" {
		t.Fatalf("只有环境上下文时指纹应为空, got %q", got)
	}
}
