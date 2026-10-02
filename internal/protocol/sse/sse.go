// Package sse 是增量 SSE 解析器。
//
// 职责单一：把字节流切成事件，不关心事件内容的语义。
// 调用方（proxy）负责解释 data 里的 JSON。
//
// 关键要求（PLAN.md §5.1）：跨 chunk 截断必须安全 ——
// 上游一个事件可能被 TCP 切成任意多段，解析器不得产生半个事件。
package sse

import (
	"bytes"
	"strings"
)

// Event 是一个完整的 SSE 事件。
type Event struct {
	// Name 是 event: 字段的值，缺省为空（默认按 "message" 处理）。
	Name string
	// Data 是所有 data: 行拼接的结果，行间以 \n 连接（不含结尾换行）。
	Data string
	// Raw 是该事件的原始字节（含结尾空行），便于原样转发。
	Raw []byte
}

// Parser 增量消费字节流。
//
// 用法：
//
//	p := &sse.Parser{}
//	for each chunk {
//	    events := p.Feed(chunk)
//	    ...
//	}
//	events := p.Flush()   // 处理末尾无空行的事件
type Parser struct {
	buf []byte
	// start 是当前未完成事件在 buf 中的起点
	start int
}

// maxEventBytes 防止恶意/异常流无限累积。
const maxEventBytes = 8 << 20 // 8 MiB

// Feed 追加一段字节，返回本次能完整解析出的事件。
//
// 返回的 Event.Raw / Data 都是新分配的副本，不引用内部缓冲，
// 因此调用方可以安全地长期持有。
func (p *Parser) Feed(chunk []byte) []Event {
	p.buf = append(p.buf, chunk...)
	return p.drain(false)
}

// Flush 在流结束时调用，产出末尾未以空行收尾的事件。
func (p *Parser) Flush() []Event {
	return p.drain(true)
}

// Pending 返回尚未构成完整事件的字节数，用于诊断与内存上限判断。
func (p *Parser) Pending() int {
	return len(p.buf) - p.start
}

func (p *Parser) drain(final bool) []Event {
	var events []Event
	for {
		// 事件以空行分隔。SSE 规范允许 \r\n、\n、\r 三种行尾。
		idx, sepLen := findEventEnd(p.buf, p.start)
		if idx < 0 {
			if final {
				// 末尾残留：若非空则当作一个事件
				tail := bytes.TrimRight(p.buf[p.start:], "\r\n")
				if len(tail) > 0 {
					if ev, ok := parseEvent(tail); ok {
						events = append(events, ev)
					}
				}
				p.buf = p.buf[:0]
				p.start = 0
			}
			return events
		}

		block := p.buf[p.start:idx]
		rawEnd := idx + sepLen
		if ev, ok := parseEvent(block); ok {
			ev.Raw = append([]byte(nil), p.buf[p.start:rawEnd]...)
			events = append(events, ev)
		}
		p.start = rawEnd

		// 回收已消费的前缀，避免无限增长
		if p.start > 64<<10 {
			p.buf = append(p.buf[:0], p.buf[p.start:]...)
			p.start = 0
		}

		if p.Pending() > maxEventBytes {
			// 单个事件超限：丢弃，避免 OOM
			p.buf = p.buf[:0]
			p.start = 0
			return events
		}
	}
}

// findEventEnd 找到从 start 起第一个空行的位置。
// 返回空行的起始下标与分隔符长度；未找到返回 (-1, 0)。
func findEventEnd(buf []byte, start int) (int, int) {
	i := start
	for i < len(buf) {
		switch buf[i] {
		case '\n':
			// \n\n 或 \n\r\n
			if i+1 < len(buf) && buf[i+1] == '\n' {
				return i, 2
			}
			if i+2 < len(buf) && buf[i+1] == '\r' && buf[i+2] == '\n' {
				return i, 3
			}
			i++
		case '\r':
			if i+1 < len(buf) && buf[i+1] == '\n' {
				// \r\n\r\n
				if i+3 < len(buf) && buf[i+2] == '\r' && buf[i+3] == '\n' {
					return i, 4
				}
				// \r\n\n
				if i+2 < len(buf) && buf[i+2] == '\n' {
					return i, 3
				}
				i += 2
				continue
			}
			// 裸 \r\r
			if i+1 < len(buf) && buf[i+1] == '\r' {
				return i, 2
			}
			i++
		default:
			i++
		}
	}
	return -1, 0
}

// parseEvent 解析一个事件块（不含结尾空行）。
func parseEvent(block []byte) (Event, bool) {
	var ev Event
	var dataLines []string
	sawField := false

	for _, line := range splitLines(block) {
		if len(line) == 0 {
			continue
		}
		if line[0] == ':' {
			continue // 注释行
		}
		name, value, found := strings.Cut(string(line), ":")
		if !found {
			// 只有字段名，无冒号：值按空串处理
			name, value = string(line), ""
		} else {
			value = strings.TrimPrefix(value, " ")
		}

		switch name {
		case "event":
			ev.Name = value
			sawField = true
		case "data":
			dataLines = append(dataLines, value)
			sawField = true
		case "id", "retry":
			sawField = true
		default:
			// 未知字段按规范忽略
		}
	}

	if !sawField {
		return Event{}, false
	}
	ev.Data = strings.Join(dataLines, "\n")
	return ev, true
}

// splitLines 按 \r\n / \n / \r 切分。
func splitLines(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i := 0; i < len(b); i++ {
		switch b[i] {
		case '\n':
			out = append(out, b[start:i])
			start = i + 1
		case '\r':
			out = append(out, b[start:i])
			if i+1 < len(b) && b[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, b[start:])
	}
	return out
}
