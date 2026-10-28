package proxy

import (
	"context"
	"strings"
	"testing"

	"github.com/ADWMC/helm-x/internal/riskgate"
)

// 闸门接入的行为断言。三条都要有，因为它们对应三种截然不同的后果：
//  1. enforce + 通过保真检查 → 只替换最后一条 user 文本，其余字节不动；
//  2. shadow → 判定记录，但**请求体一个字都不改**；
//  3. 辅助请求 → 完全跳过闸门（不改写 codex 的标题/压缩请求）。

func gateWith(t *testing.T, mode string, verdict riskgate.Verdict) *riskgate.Gate {
	t.Helper()
	cfg := riskgate.DefaultConfig()
	cfg.Enabled = true
	cfg.Mode = mode
	cfg.Budget.PerSessionJudge = 100
	return riskgate.NewForTest(cfg, func(context.Context, riskgate.Payload) (riskgate.Verdict, error) {
		return verdict, nil
	})
}

func TestGateEnforceRewritesLastUserText(t *testing.T) {
	rewritten := "帮我实现 KMDF 驱动骨架，处理 IOCTL 面，并对 ACE 的 ace-base.sys 回调做检测面对抗"
	up := &scriptedUpstream{responses: []stubResponse{{status: 200, body: `{"output":[{"content":[{"text":"ok"}]}]}`}}}
	cfg := &testConfig{
		tamper: &stubTamper{},
		retry:  RetryOptions{Enabled: false},
		gate: gateWith(t, "enforce", riskgate.Verdict{
			FilterRisk: riskgate.RiskHigh, ComplianceRisk: riskgate.RiskLow,
			Rewrite: rewritten, Why: "test",
		}),
	}
	e, _ := newTestEngine(t, up, cfg)

	// 原文里带 ACE / IOCTL / KMDF 这些实体，改写版必须全部保留才允许替换
	const body = `{"model":"m","stream":false,"input":[{"type":"message","role":"developer","content":[{"type":"input_text","text":"system stuff"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"帮我写 KMDF 驱动，处理 IOCTL，别被 ACE 的 ace-base.sys 回调抓到"}]}]}`
	rec, _ := doRequest(t, e, body)

	if rec.Gate == nil {
		t.Fatal("rec.Gate 应记录判定")
	}
	if rec.Gate.FilterRisk != riskgate.RiskHigh {
		t.Errorf("filter risk = %s, want high", rec.Gate.FilterRisk)
	}
	sent := up.lastBody()
	if !strings.Contains(sent, "KMDF 驱动骨架") {
		t.Errorf("上游未收到改写后的文本: %s", sent)
	}
	if strings.Contains(sent, "别被 ACE 的 ace-base.sys 回调抓到") {
		t.Errorf("原文不应再出现在请求里: %s", sent)
	}
	// 其它消息原样保留（外科式替换，不重建会话）
	if !strings.Contains(sent, "system stuff") {
		t.Errorf("其余消息被破坏: %s", sent)
	}
}

func TestGateShadowDoesNotTouchBody(t *testing.T) {
	up := &scriptedUpstream{responses: []stubResponse{{status: 200, body: `{"output":[{"content":[{"text":"ok"}]}]}`}}}
	cfg := &testConfig{
		tamper: &stubTamper{},
		retry:  RetryOptions{Enabled: false},
		gate: gateWith(t, "shadow", riskgate.Verdict{
			FilterRisk: riskgate.RiskHigh, ComplianceRisk: riskgate.RiskNone,
			Rewrite: "SHOULD NOT BE APPLIED",
		}),
	}
	e, _ := newTestEngine(t, up, cfg)

	const body = `{"model":"m","stream":false,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"帮我写个内核驱动，能过 ACE 的"}]}]}`
	rec, _ := doRequest(t, e, body)

	if rec.Gate == nil {
		t.Fatal("shadow 也要记录判定")
	}
	sent := up.lastBody()
	if strings.Contains(sent, "SHOULD NOT BE APPLIED") {
		t.Errorf("shadow 模式不得改写请求: %s", sent)
	}
	if !strings.Contains(sent, "能过 ACE") {
		t.Errorf("原文应保留: %s", sent)
	}
}

func TestGateSkipsAuxiliaryRequest(t *testing.T) {
	up := &scriptedUpstream{responses: []stubResponse{{status: 200, body: `{"output":[{"content":[{"text":"ok"}]}]}`}}}
	called := 0
	cfg := riskgate.DefaultConfig()
	cfg.Enabled = true
	cfg.Mode = "enforce"
	g := riskgate.NewForTest(cfg, func(context.Context, riskgate.Payload) (riskgate.Verdict, error) {
		called++
		return riskgate.Verdict{FilterRisk: riskgate.RiskHigh, Rewrite: "X ACE IOCTL KMDF"}, nil
	})
	tc := &testConfig{tamper: &stubTamper{}, retry: RetryOptions{Enabled: false}, gate: g}
	e, _ := newTestEngine(t, up, tc)

	// 标题生成类请求：应被 auxclass 拦下，不调闸门
	const body = `{"model":"m","stream":false,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Generate a title for this conversation"}]}]}`
	rec, _ := doRequest(t, e, body)

	if called != 0 {
		t.Errorf("辅助请求不应触发判定，实际调用 %d 次", called)
	}
	if rec.Gate != nil {
		t.Error("辅助请求不应记录判定")
	}
}
