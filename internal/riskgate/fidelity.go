package riskgate

import (
	"regexp"
	"strings"
)

// 保真检查：改写不许把"要什么"改没。
//
// 为什么必须有（docs/RISKGATE-DESIGN.md §3.2）：改写的失败模式不是"没改好"，
// 而是"把请求改成了另一个请求"。实体丢一个，交付就变形，而模型不会告诉你。
// 所以每次改写后断言：原文实体全部仍在，产物名不变；否则改写作废、回退原文。

// 实体类别。顺序即报告顺序。
var entityPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{"产品", regexp.MustCompile(`(?i)\b(ACE|EAC|BattlEye|hCaptcha|reCAPTCHA|Turnstile|Steam|Wails|Codex|Frida|UPX|Ollama|ONNX|OpenCV)\b`)},
	{"寄存器/结构", regexp.MustCompile(`(?i)\b(CR[0-9]|MSR|EPROCESS|KPROCESS|VAD|PTE|PML4|IDT|SSDT|IOCTL|KMDF|WDM|IRP)\b`)},
	{"数值", regexp.MustCompile(`(?i)\b(0x[0-9a-f]{1,16}|[0-9a-f]{8,32})\b`)},
	// 文件名整取（ace-base.sys / SteamSignup.vue），不要只抓扩展名——只抓 ".sys" 会丢失主体名，
	// 改写后即使驱动名被换掉也断言不出来。
	{"文件", regexp.MustCompile(`(?i)\b[\w-]+(?:\.[\w-]+)*\.(sys|exe|dll|apk|elf|onnx|pt|engine|safetensors|json|toml|yaml|md|go|py|ts|vue)\b`)},
	{"网络", regexp.MustCompile(`\b\d{1,3}(\.\d{1,3}){3}(:\d{2,5})?\b`)},
	{"协议/字段", regexp.MustCompile(`(?i)\b(SSE|HTTP/[12]|protobuf|pcap|tcp|udp|smb|kerberos|ntlm|jwt|oauth)\b`)},
}

// Entities 抽取文本中的技术实体（去重、保序）。
func Entities(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, ep := range entityPatterns {
		for _, m := range ep.re.FindAllString(text, -1) {
			key := strings.ToLower(m)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, m)
		}
	}
	return out
}

// Retention 计算改写后实体保留率（1.0 表示一个没丢）。
func Retention(original, rewritten string) (kept int, total int, rate float64) {
	ents := Entities(original)
	if len(ents) == 0 {
		return 0, 0, 1
	}
	lower := strings.ToLower(rewritten)
	for _, e := range ents {
		if strings.Contains(lower, strings.ToLower(e)) {
			kept++
		}
	}
	return kept, len(ents), float64(kept) / float64(len(ents))
}

// FidelityOK 断言改写可用：实体全保留，且产物词没有被替换成别的东西。
func FidelityOK(original, rewritten string) (bool, string) {
	if strings.TrimSpace(rewritten) == "" {
		return false, "改写为空"
	}
	_, total, rate := Retention(original, rewritten)
	if rate < 1.0 {
		return false, "实体丢失"
	}
	if total == 0 && len(rewritten) < len(original)/3 {
		return false, "改写过度缩短且无实体可比对"
	}
	return true, ""
}
