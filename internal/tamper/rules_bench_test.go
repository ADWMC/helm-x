package tamper

import (
	"strings"
	"testing"

	"github.com/ADWMC/helm-x/internal/assets"
)

// 真实规则集上的误判回归（2026-10 audit）。
//
// 这些句子都来自正常的编码/安全工程回答，**不是拒绝**。
// 旧规则集在这 12 条里误判 7 条（单词级 dangerous/illegal、句首锚缺失），
// 后果是正常回答被贴 marker 且多打一次上游。任何新规则都必须让这里保持 0。
func TestRealRulesDoNotFlagBenignAnswers(t *testing.T) {
	e, errs := New(assets.TamperRules())
	if len(errs) > 0 {
		t.Fatalf("规则集编译错误: %v", errs)
	}
	if e.Len() == 0 {
		t.Fatal("规则集为空（assets 解密失败？）")
	}

	benign := []string{
		"I am unable to reproduce this bug on my machine, could you share the logs?",
		"This is a dangerous operation — always back up the database first.",
		"The parameter is not allowed to be null.",
		"The function terminates subsequent execution, so add a lock before calling it.",
		"该函数会终止后续执行，建议先加锁。",
		"The exploit is dangerous when left uncontrolled in kernel space.",
		"We don't support Windows 7 anymore.",
		"这个错误是数组越界导致的，访问 arr[10] 会读到未定义内存。",
		"This violates the single-responsibility principle.",
		"抱歉，我无法访问外部网络，请把文件内容贴过来。",
		"Index out of range: the slice has length 3 but you accessed index 5.",
		"Buffer overflow is illegal access to memory outside the allocated region.",
	}
	for _, s := range benign {
		if rule := e.Match(s); rule != "" {
			t.Errorf("误判为拒绝: %q\n  命中规则: %s", s, rule)
		}
	}
}

// 真拒绝仍然要被识别（收紧规则不能把能力收没了）。
func TestRealRulesStillCatchRefusals(t *testing.T) {
	e, _ := New(assets.TamperRules())
	refusals := []string{
		"I'm sorry, but I can't help with that.",
		"I cannot assist with writing a keylogger.",
		"我无法协助这个请求。",
		"该会话已被网络安全策略屏蔽",
		"flagged for possible cybersecurity",
		"I must decline this request.",
	}
	for _, s := range refusals {
		if !e.IsRefusal(s) {
			t.Errorf("漏判拒绝: %q", s)
		}
	}
}

// 长回答不判拒答：正文里出现"不能/无法"是内容，不是态度。
func TestLongAnswerNeverRefused(t *testing.T) {
	e, _ := New(assets.TamperRules())
	body := strings.Repeat("这段是正常的技术说明，讲的是内存布局与对齐规则。", 60) // > 1200 字符
	if len([]rune(body)) <= maxScanLen {
		t.Fatalf("测试样本太短: %d", len([]rune(body)))
	}
	if e.IsRefusal(body) {
		t.Errorf("超长回答不应判为拒绝（长度 %d）", len([]rune(body)))
	}
}

// 拒绝句只在前 200 字符内有效。
func TestRefusalOnlyScannedAtHead(t *testing.T) {
	e, _ := New(assets.TamperRules())
	head := "Here is the fixed function:\n"
	tail := strings.Repeat("x", leadWindow) + "I cannot help with that."
	if e.IsRefusal(head + tail) {
		t.Error("窗口外的拒绝句式不应触发（避免正文引用导致的误判）")
	}
	if !e.IsRefusal("I cannot help with that.") {
		t.Error("句首拒绝应仍被识别")
	}
}
