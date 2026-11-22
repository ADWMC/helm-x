package proxy

import (
	"strings"
	"testing"
)

// 管线级部署语义测试：断言**发往上游的字节**，不是内部状态。

func deployPipelineBody() string {
	return `{"model":"m","stream":false,"instructions":"OFFICIAL-BASE","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`
}

func TestPipelineDeployReplaceBase(t *testing.T) {
	up := &scriptedUpstream{responses: []stubResponse{{
		status: 200,
		body:   `{"output":[{"content":[{"text":"ok"}]}]}`,
	}}}
	cfg := &testConfig{
		instruction: "ARM-TEXT-28K", deploy: "replace-base",
		tamper: &stubTamper{}, retry: RetryOptions{Enabled: false},
	}
	e, _ := newTestEngine(t, up, cfg)
	rec, _ := doRequest(t, e, deployPipelineBody())

	if !rec.Injected || rec.Deploy != "replace-base" {
		t.Fatalf("injected=%v deploy=%q, want true/replace-base", rec.Injected, rec.Deploy)
	}
	sent := up.lastBody()
	if !strings.Contains(sent, `"instructions":"ARM-TEXT-28K"`) {
		t.Errorf("上游未收到整段替换后的 instructions: %s", sent)
	}
	if strings.Contains(sent, "OFFICIAL-BASE") {
		t.Errorf("官方 base 未被替换: %s", sent)
	}
	// 其余字段必须原样
	if !strings.Contains(sent, `"input":[{"type":"message","role":"user"`) {
		t.Errorf("input 段被改动: %s", sent)
	}
	if !strings.Contains(sent, `"model":"m"`) {
		t.Errorf("model 丢失: %s", sent)
	}
}

func TestPipelineDeployAppend(t *testing.T) {
	up := &scriptedUpstream{responses: []stubResponse{{
		status: 200,
		body:   `{"output":[{"content":[{"text":"ok"}]}]}`,
	}}}
	cfg := &testConfig{
		instruction: "TAIL-MARK", deploy: "append",
		tamper: &stubTamper{}, retry: RetryOptions{Enabled: false},
	}
	e, _ := newTestEngine(t, up, cfg)
	rec, _ := doRequest(t, e, deployPipelineBody())

	if rec.Deploy != "append" {
		t.Fatalf("deploy = %q, want append", rec.Deploy)
	}
	sent := up.lastBody()
	if !strings.Contains(sent, "OFFICIAL-BASE") {
		t.Errorf("append 不应删掉官方 base: %s", sent)
	}
	if !strings.Contains(sent, "TAIL-MARK") {
		t.Errorf("append 未写入指令: %s", sent)
	}
	// 追加必须在 instructions 内，不是另起一条 system 消息
	if strings.Contains(sent, `"role":"system"`) {
		t.Errorf("append 档不应往 input 插 system 消息: %s", sent)
	}
}

// insert 档（默认）保持历史行为：instructions 不动，input 首条变 system。
func TestPipelineDeployInsertDefault(t *testing.T) {
	up := &scriptedUpstream{responses: []stubResponse{{
		status: 200,
		body:   `{"output":[{"content":[{"text":"ok"}]}]}`,
	}}}
	cfg := &testConfig{
		instruction: "IN-INPUT", deploy: "",
		tamper: &stubTamper{}, retry: RetryOptions{Enabled: false},
	}
	e, _ := newTestEngine(t, up, cfg)
	rec, _ := doRequest(t, e, deployPipelineBody())

	if rec.Deploy != "insert" {
		t.Fatalf("deploy = %q, want insert", rec.Deploy)
	}
	sent := up.lastBody()
	if !strings.Contains(sent, `"instructions":"OFFICIAL-BASE"`) {
		t.Errorf("insert 档不应改 instructions: %s", sent)
	}
	if !strings.Contains(sent, `"role":"system"`) {
		t.Errorf("insert 档应往 input 插 system 消息: %s", sent)
	}
}
