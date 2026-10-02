package sse

import (
	"strings"
	"testing"
)

func dataOf(evs []Event) []string {
	out := make([]string, 0, len(evs))
	for _, e := range evs {
		out = append(out, e.Data)
	}
	return out
}

func TestSingleEvent(t *testing.T) {
	p := &Parser{}
	evs := p.Feed([]byte("data: hello\n\n"))
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1", len(evs))
	}
	if evs[0].Data != "hello" {
		t.Errorf("Data = %q, want hello", evs[0].Data)
	}
}

func TestMultipleEventsInOneChunk(t *testing.T) {
	p := &Parser{}
	evs := p.Feed([]byte("data: a\n\ndata: b\n\ndata: c\n\n"))
	got := dataOf(evs)
	want := []string{"a", "b", "c"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

// ★ 核心要求：跨 chunk 截断必须安全，不得产出半个事件。
func TestSplitAcrossChunks(t *testing.T) {
	full := "data: first\n\ndata: second\n\n"

	// 在每个可能的切点断开，结果必须一致
	for cut := 0; cut <= len(full); cut++ {
		p := &Parser{}
		var got []string
		got = append(got, dataOf(p.Feed([]byte(full[:cut])))...)
		got = append(got, dataOf(p.Feed([]byte(full[cut:])))...)
		got = append(got, dataOf(p.Flush())...)

		want := []string{"first", "second"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("cut=%d: got %v, want %v", cut, got, want)
		}
	}
}

// 逐字节喂入，模拟最坏情况的 TCP 分片。
func TestByteByByte(t *testing.T) {
	full := "data: alpha\n\ndata: beta\n\n"
	p := &Parser{}
	var got []string
	for i := 0; i < len(full); i++ {
		got = append(got, dataOf(p.Feed([]byte(full[i:i+1])))...)
	}
	got = append(got, dataOf(p.Flush())...)

	want := []string{"alpha", "beta"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

// 多行 data 按规范以 \n 连接。
func TestMultilineData(t *testing.T) {
	p := &Parser{}
	evs := p.Feed([]byte("data: line1\ndata: line2\n\n"))
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1", len(evs))
	}
	if evs[0].Data != "line1\nline2" {
		t.Errorf("Data = %q, want %q", evs[0].Data, "line1\nline2")
	}
}

func TestEventName(t *testing.T) {
	p := &Parser{}
	evs := p.Feed([]byte("event: ping\ndata: {}\n\n"))
	if len(evs) != 1 {
		t.Fatalf("got %d events", len(evs))
	}
	if evs[0].Name != "ping" {
		t.Errorf("Name = %q, want ping", evs[0].Name)
	}
}

// 三种行尾都要支持。
func TestLineEndings(t *testing.T) {
	cases := []string{
		"data: x\n\n",
		"data: x\r\n\r\n",
		"data: x\r\r",
	}
	for _, c := range cases {
		p := &Parser{}
		evs := p.Feed([]byte(c))
		if len(evs) != 1 || evs[0].Data != "x" {
			t.Errorf("input %q: got %v", c, dataOf(evs))
		}
	}
}

func TestCommentsIgnored(t *testing.T) {
	p := &Parser{}
	evs := p.Feed([]byte(": keepalive\ndata: real\n\n"))
	if len(evs) != 1 || evs[0].Data != "real" {
		t.Errorf("got %v, want [real]", dataOf(evs))
	}
}

// 纯注释块不应产出事件（各家上游常发心跳）。
func TestCommentOnlyBlockProducesNoEvent(t *testing.T) {
	p := &Parser{}
	evs := p.Feed([]byte(": ping\n\n"))
	if len(evs) != 0 {
		t.Errorf("got %d events, want 0", len(evs))
	}
}

// 无冒号字段名按空值处理。
func TestFieldWithoutColon(t *testing.T) {
	p := &Parser{}
	evs := p.Feed([]byte("data\n\n"))
	if len(evs) != 1 {
		t.Fatalf("got %d events", len(evs))
	}
	if evs[0].Data != "" {
		t.Errorf("Data = %q, want empty", evs[0].Data)
	}
}

// 值前导单个空格按规范去除，之后的空格保留。
func TestLeadingSpaceStripped(t *testing.T) {
	p := &Parser{}
	evs := p.Feed([]byte("data:   two-spaces\n\n"))
	if len(evs) != 1 {
		t.Fatalf("got %d events", len(evs))
	}
	if evs[0].Data != "  two-spaces" {
		t.Errorf("Data = %q, want %q", evs[0].Data, "  two-spaces")
	}
}

func TestFlushHandlesUnterminated(t *testing.T) {
	p := &Parser{}
	if evs := p.Feed([]byte("data: tail")); len(evs) != 0 {
		t.Fatalf("incomplete event should not be emitted, got %v", dataOf(evs))
	}
	evs := p.Flush()
	if len(evs) != 1 || evs[0].Data != "tail" {
		t.Errorf("Flush got %v, want [tail]", dataOf(evs))
	}
}

// Raw 必须完整覆盖原始字节，便于原样转发。
func TestRawIsComplete(t *testing.T) {
	src := "data: a\n\ndata: b\n\n"
	p := &Parser{}
	evs := p.Feed([]byte(src))
	if len(evs) != 2 {
		t.Fatalf("got %d events", len(evs))
	}
	if string(evs[0].Raw) != "data: a\n\n" {
		t.Errorf("Raw[0] = %q", evs[0].Raw)
	}
	if string(evs[1].Raw) != "data: b\n\n" {
		t.Errorf("Raw[1] = %q", evs[1].Raw)
	}
}

// 返回的 Data/Raw 不得引用内部缓冲（调用方可能长期持有）。
func TestReturnedSlicesDoNotAliasInternalBuffer(t *testing.T) {
	p := &Parser{}
	evs := p.Feed([]byte("data: original\n\n"))
	if len(evs) != 1 {
		t.Fatal("expected one event")
	}
	// 继续投喂大量数据，触发内部缓冲回收
	big := strings.Repeat("data: x\n\n", 5000)
	p.Feed([]byte(big))

	if evs[0].Data != "original" {
		t.Errorf("Data was mutated: %q", evs[0].Data)
	}
	if string(evs[0].Raw) != "data: original\n\n" {
		t.Errorf("Raw was mutated: %q", evs[0].Raw)
	}
}

// 真实上游形态：OpenAI Responses API 的流式事件。
func TestRealisticResponsesStream(t *testing.T) {
	stream := strings.Join([]string{
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","delta":"Hello"}`,
		``,
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","delta":" world"}`,
		``,
		`event: response.completed`,
		`data: {"type":"response.completed","response":{"id":"resp_1"}}`,
		``,
		``,
	}, "\n")

	p := &Parser{}
	evs := p.Feed([]byte(stream))
	if len(evs) != 3 {
		t.Fatalf("got %d events, want 3: %v", len(evs), dataOf(evs))
	}
	if !strings.Contains(evs[0].Data, "Hello") {
		t.Errorf("event 0 = %q", evs[0].Data)
	}
	if !strings.Contains(evs[2].Data, "resp_1") {
		t.Errorf("event 2 = %q", evs[2].Data)
	}
}

func FuzzFeed(f *testing.F) {
	f.Add([]byte("data: x\n\n"))
	f.Add([]byte("data: a\n\ndata: b\n\n"))
	f.Add([]byte(": comment\n\n"))
	f.Add([]byte("data: 中文\r\n\r\n"))
	f.Add([]byte("event: e\ndata: v\n\n"))

	f.Fuzz(func(t *testing.T, src []byte) {
		// 不变式：任意切分方式产出的事件序列必须一致。
		// 这里用「一次喂完」与「逐字节喂」对比。
		p1 := &Parser{}
		whole := p1.Feed(src)
		whole = append(whole, p1.Flush()...)

		p2 := &Parser{}
		var chunked []Event
		for i := 0; i < len(src); i++ {
			chunked = append(chunked, p2.Feed(src[i:i+1])...)
		}
		chunked = append(chunked, p2.Flush()...)

		if len(whole) != len(chunked) {
			t.Fatalf("event count differs: whole=%d chunked=%d", len(whole), len(chunked))
		}
		for i := range whole {
			if whole[i].Data != chunked[i].Data {
				t.Fatalf("event %d data differs:\n whole=%q\n chunked=%q", i, whole[i].Data, chunked[i].Data)
			}
			if whole[i].Name != chunked[i].Name {
				t.Fatalf("event %d name differs: %q vs %q", i, whole[i].Name, chunked[i].Name)
			}
		}
	})
}
