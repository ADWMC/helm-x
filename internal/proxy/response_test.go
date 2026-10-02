package proxy

import (
	"net/http"
	"strings"
	"testing"
)

func hdr(ct string) http.Header {
	h := http.Header{}
	if ct != "" {
		h.Set("Content-Type", ct)
	}
	return h
}

// 拒绝判定替身：只把含"无法协助"的文本当拒绝。
func refusalStub(s string) bool { return strings.Contains(s, "无法协助") }

func TestClassify(t *testing.T) {
	okJSON := `{"id":"r","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"正常输出"}]}]}`
	refuseJSON := `{"id":"r","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"抱歉，我无法协助这个请求。"}]}]}`
	errJSON := `{"error":{"message":"boom","type":"server_error"}}`
	cyberJSON := `{"error":{"message":"flagged for possible cybersecurity","type":"policy"}}`

	cases := []struct {
		name  string
		in    ClassifyInput
		want  Class
		notes string
	}{
		{
			name: "不完整响应永远是上游失败",
			in: ClassifyInput{Status: 200, Header: hdr("application/json"),
				Body: []byte(okJSON), Complete: false, IsRefusal: refusalStub, IsFlagged: classifyIsFlagged},
			want: ClassUpstreamFailed,
		},
		{
			name: "2xx 正常内容",
			in: ClassifyInput{Status: 200, Header: hdr("application/json"),
				Body: []byte(okJSON), Complete: true, IsRefusal: refusalStub, IsFlagged: classifyIsFlagged},
			want: ClassHealthy,
		},
		{
			name: "2xx 命中拒绝规则",
			in: ClassifyInput{Status: 200, Header: hdr("application/json"),
				Body: []byte(refuseJSON), Complete: true, IsRefusal: refusalStub, IsFlagged: classifyIsFlagged},
			want: ClassRefused,
		},
		{
			name: "502 错误体是上游失败，绝不判为拒绝",
			in: ClassifyInput{Status: 502, Header: hdr("application/json"),
				Body: []byte(errJSON), Complete: true, IsRefusal: refusalStub, IsFlagged: classifyIsFlagged},
			want: ClassUpstreamFailed,
		},
		{
			name: "5xx 且命中安全标记",
			in: ClassifyInput{Status: 403, Header: hdr("application/json"),
				Body: []byte(cyberJSON), Complete: true, IsRefusal: refusalStub, IsFlagged: classifyIsFlagged},
			want: ClassFlagged,
		},
		{
			name: "200 空响应是失败",
			in: ClassifyInput{Status: 200, Header: hdr("application/json"),
				Body: []byte("   "), Complete: true, IsRefusal: refusalStub, IsFlagged: classifyIsFlagged},
			want: ClassUpstreamFailed,
		},
		{
			name: "200 非法 JSON 是 Malformed",
			in: ClassifyInput{Status: 200, Header: hdr("application/json"),
				Body: []byte(`{not json`), Complete: true, IsRefusal: refusalStub, IsFlagged: classifyIsFlagged},
			want: ClassMalformed,
		},
		{
			name: "200 SSE 正常",
			in: ClassifyInput{Status: 200, Header: hdr("text/event-stream"),
				Body:     []byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"你好\"}\n\n"),
				Complete: true, IsRefusal: refusalStub, IsFlagged: classifyIsFlagged},
			want: ClassHealthy,
		},
		{
			name: "200 SSE 命中拒绝",
			in: ClassifyInput{Status: 200, Header: hdr("text/event-stream"),
				Body:     []byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"我无法协助\"}\n\n"),
				Complete: true, IsRefusal: refusalStub, IsFlagged: classifyIsFlagged},
			want: ClassRefused,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Classify(c.in)
			if got.Class != c.want {
				t.Errorf("Classify = %s, want %s (reason=%q)", got.Class, c.want, got.Reason)
			}
		})
	}
}

// ★ 核心不变量：非 2xx 永远不能产生 Refused。
// 这是修 P8 语义不一致的关键 —— 旧版对重试结果的判定与非流式路径规则不同。
func TestNonSuccessfulNeverRefused(t *testing.T) {
	// 一个同时含拒绝词和安全标记词的错误体
	body := []byte(`{"error":{"message":"抱歉我无法协助，flagged for possible cybersecurity"}}`)

	// 4xx：安全标记是**策略性**拒绝，值得重建会话（与旧版语义一致）
	for _, status := range []int{400, 401, 403, 408, 429} {
		got := Classify(ClassifyInput{
			Status: status, Header: hdr("application/json"), Body: body,
			Complete: true, IsRefusal: refusalStub, IsFlagged: classifyIsFlagged,
		})
		if got.Class == ClassRefused {
			t.Errorf("status=%d 被判为 Refused —— 非 2xx 绝不应产生 Refused", status)
		}
		if got.Class != ClassFlagged {
			t.Errorf("status=%d 含安全标记应为 Flagged，得到 %s", status, got.Class)
		}
	}

	// 5xx：服务端故障，不是策略拒绝。标记词不改变这一点 ——
	// 正确处置是重试，而不是重建会话。这是有意的行为差异。
	for _, status := range []int{500, 502, 503} {
		got := Classify(ClassifyInput{
			Status: status, Header: hdr("application/json"), Body: body,
			Complete: true, IsRefusal: refusalStub, IsFlagged: classifyIsFlagged,
		})
		if got.Class != ClassUpstreamFailed {
			t.Errorf("status=%d 是服务端故障，应为 UpstreamFailed，得到 %s", status, got.Class)
		}
	}
}

// UpstreamFailed 必须能被识别出来，供上层跳过 TAMPER。
func TestUpstreamFailedIsDistinct(t *testing.T) {
	for _, in := range []ClassifyInput{
		{Status: 0, Complete: false},
		{Status: 502, Complete: true, Body: []byte(`{"error":{}}`), Header: hdr("application/json")},
		{Status: 200, Complete: true, Body: []byte(""), Header: hdr("application/json")},
	} {
		got := Classify(in)
		if got.Class != ClassUpstreamFailed {
			t.Errorf("status=%d complete=%v body=%q → %s，want UpstreamFailed",
				in.Status, in.Complete, in.Body, got.Class)
		}
	}
}

func TestExtractText(t *testing.T) {
	cases := []struct {
		name string
		body string
		sse  bool
		want string
	}{
		{
			name: "output 嵌套",
			body: `{"output":[{"content":[{"text":"甲"},{"text":"乙"}]}]}`,
			want: "甲乙",
		},
		{
			name: "顶层 output_text",
			body: `{"output_text":"直接"}`,
			want: "直接",
		},
		{
			name: "错误体取 message",
			body: `{"error":{"message":"出了点问题"}}`,
			want: "出了点问题",
		},
		{
			name: "SSE delta 拼接",
			body: "data: {\"delta\":\"你\"}\n\ndata: {\"delta\":\"好\"}\n\n",
			sse:  true,
			want: "你好",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractText([]byte(c.body), c.sse); got != c.want {
				t.Errorf("extractText = %q, want %q", got, c.want)
			}
		})
	}
}

// isSSEContent 优先信 Content-Type，其次看内容。
func TestIsSSEContent(t *testing.T) {
	if !isSSEContent(hdr("text/event-stream; charset=utf-8"), []byte("x")) {
		t.Error("Content-Type 应为权威依据")
	}
	if !isSSEContent(hdr(""), []byte("data: x\n\n")) {
		t.Error("无 Content-Type 时应按内容识别")
	}
	if isSSEContent(hdr("application/json"), []byte(`{"a":1}`)) {
		t.Error("JSON 不应判为 SSE")
	}
}
