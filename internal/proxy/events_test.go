package proxy

import (
	"testing"

	"github.com/ADWMC/helm-x/internal/protocol/sse"
)

func TestCollectUpstreamEvent(t *testing.T) {
	var rec RequestRecord
	collectUpstreamEvent(&rec, sse.Event{Name: "response.output_text.delta", Data: `{"delta":"hi"}`})
	if len(rec.UpstreamMeta) != 0 {
		t.Fatalf("内容事件不该被摘录: %v", rec.UpstreamMeta)
	}

	collectUpstreamEvent(&rec, sse.Event{Name: "rate_limits", Data: `{"plan":"plus","used":12,"limit":150}`})
	if got := rec.UpstreamMeta["rate_limits"]; got == "" || !contains(got, `"used":12`) {
		t.Fatalf("rate_limits 未摘录: %q", got)
	}

	// 同一事件再来一次：保留第一条
	collectUpstreamEvent(&rec, sse.Event{Name: "rate_limits", Data: `{"used":99}`})
	if got := rec.UpstreamMeta["rate_limits"]; !contains(got, `"used":12`) {
		t.Errorf("应保留第一条，得到 %q", got)
	}

	// response.completed 只摘 id/status/usage，且用最后一条覆盖
	collectUpstreamEvent(&rec, sse.Event{
		Name: "response.completed",
		Data: `{"id":"resp_1","status":"completed","output":[{"huge":"正文"}],"usage":{"input_tokens":10,"output_tokens":20}}`,
	})
	got := rec.UpstreamMeta["response.completed"]
	if !contains(got, "resp_1") || !contains(got, "usage") {
		t.Fatalf("response.completed 摘录不完整: %q", got)
	}
	if contains(got, "huge") {
		t.Errorf("不应复制响应正文: %q", got)
	}
}

func TestCollectUpstreamEventTruncates(t *testing.T) {
	var rec RequestRecord
	long := `{"k":"`
	for i := 0; i < 2000; i++ {
		long += "x"
	}
	long += `"}`
	collectUpstreamEvent(&rec, sse.Event{Name: "response.metadata", Data: long})
	got := rec.UpstreamMeta["response.metadata"]
	if len(got) > maxMetaBytes+8 {
		t.Errorf("未截断: %d 字节", len(got))
	}
}

func TestCollectUpstreamEventIgnoresEmpty(t *testing.T) {
	var rec RequestRecord
	collectUpstreamEvent(&rec, sse.Event{Name: "rate_limits", Data: "  "})
	collectUpstreamEvent(&rec, sse.Event{Name: "", Data: "{}"})
	if len(rec.UpstreamMeta) != 0 {
		t.Errorf("空事件不该入库: %v", rec.UpstreamMeta)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
