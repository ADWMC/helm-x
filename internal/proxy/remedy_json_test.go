package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

// P1-7：非流式标记必须做**字节级**插入，不能重序列化。
//
// 旧实现走 json.Unmarshal → map → Marshal，把 {"content":…,"type":…}
// 重排成 {"type":…,"content":…}，既破坏 INV-4，也会让做响应签名/哈希的
// 客户端直接失败。这里用一份"key 顺序刻意反常规 + 带未知字段"的响应来锁住。
func TestAttachMarkerJSONKeepsBytes(t *testing.T) {
	const marker = "[M]\n"
	src := `{"output":[{"content":[{"text":"hello","type":"output_text"}],"type":"message","id":"x1","extra":{"z":1,"a":2}}],"usage":{"in":1,"out":2}}`

	st := ResponseState{Class: ClassRefused, Status: 200, rawBody: []byte(src), Text: "hello"}
	out, ok := attachMarkerJSON(st, marker)
	if !ok {
		t.Fatal("应能附加标记")
	}

	want := strings.Replace(src, `"text":"hello"`, `"text":"[M]\nhello"`, 1)
	if string(out.rawBody) != want {
		t.Errorf("字节级插入不符\n got: %s\nwant: %s", out.rawBody, want)
	}
	if !json.Valid(out.rawBody) {
		t.Errorf("产物不是合法 JSON: %s", out.rawBody)
	}
	if out.Text != marker+"hello" {
		t.Errorf("Text = %q", out.Text)
	}
}

// 顶层 output_text / text 的退化路径同样保持其余字节。
func TestAttachMarkerJSONFallsBackToTopLevel(t *testing.T) {
	const marker = "[M]\n"
	src := `{"created":123,"output_text":"answer","other":[1,2,3]}`
	st := ResponseState{Class: ClassRefused, Status: 200, rawBody: []byte(src), Text: "answer"}

	out, ok := attachMarkerJSON(st, marker)
	if !ok {
		t.Fatal("应能附加标记（退化到 output_text）")
	}
	want := `{"created":123,"output_text":"[M]\nanswer","other":[1,2,3]}`
	if string(out.rawBody) != want {
		t.Errorf("got: %s\nwant: %s", out.rawBody, want)
	}
}

// 认不出的形态不猜：返回 false，由调用方原样透传（INV-9）。
func TestAttachMarkerJSONRefusesUnknownShape(t *testing.T) {
	st := ResponseState{Class: ClassRefused, Status: 200, rawBody: []byte(`{"foo":"bar"}`), Text: "bar"}
	if _, ok := attachMarkerJSON(st, "[M]\n"); ok {
		t.Error("找不到正文时不应硬塞标记")
	}
	st2 := ResponseState{Class: ClassRefused, Status: 200, rawBody: []byte(`not json`)}
	if _, ok := attachMarkerJSON(st2, "[M]\n"); ok {
		t.Error("非 JSON 不应硬塞标记")
	}
}

// P2-11：TamperMatcher 缺失时不能 panic，也不能假装补救成功。
func TestAttachMarkerWithNilTamperIsNoop(t *testing.T) {
	st := ResponseState{Class: ClassRefused, Status: 200, rawBody: []byte(`{"output_text":"x"}`)}
	if _, ok := attachMarkerToState(st, noopTamper{}); ok {
		t.Error("没有 marker 时不应声称已附加")
	}
}

// 流式标记：只前置一个事件，原文一个字节都不动（INV-8）。
func TestAttachMarkerSSEPrependsOnly(t *testing.T) {
	body := "event: response.output_text.delta\ndata: {\"delta\":\"原文\"}\n\n"
	st := ResponseState{Class: ClassRefused, Status: 200, SSE: true, rawBody: []byte(body), Text: "原文"}
	out, ok := attachMarkerToState(st, stubMarker{})
	if !ok {
		t.Fatal("流式应能附加标记")
	}
	if !strings.HasSuffix(string(out.rawBody), body) {
		t.Errorf("原文必须原样保留在尾部:\n%s", out.rawBody)
	}
	if !strings.HasPrefix(string(out.rawBody), "event: response.output_text.delta\ndata: ") {
		t.Errorf("标记应是合法 SSE 事件:\n%.80s", out.rawBody)
	}
}

// stubMarker 是最小 TamperMatcher 实现。
type stubMarker struct{}

func (stubMarker) IsRefusal(string) bool  { return true }
func (stubMarker) Attach(s string) string { return "[M]\n" + s }
func (stubMarker) Marker() string         { return "[M]\n" }
