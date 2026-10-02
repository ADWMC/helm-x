// Package jsonwalk 是保序 JSON 扫描器。
//
// 设计目标（PLAN.md §5.1）：在原始字节上定位与替换字段，**不做反序列化**。
//
// 为什么不用 encoding/json：
//   - map[string]any 往返会打乱 key 顺序
//   - 未知字段语义会丢失
//   - 大整数会经过 float64 而失真
//
// 因此本包只做只读遍历 + 原始区间记录，改写一律是字节区间替换。
// 任何解析失败都返回 error；调用方必须原样透传（INV-9）。
//
// 文件职责：
//
//	parse.go   解析器：字节流 → Value 树（只读遍历）
//	access.go  取值：Member / Index / Lookup / Walk
//	escape.go  字符串编解码：\uXXXX、代理对、最少转义
//	mutate.go  改写：Replace / Insert / Append / Delete / Splice
package jsonwalk

import (
	"errors"
	"fmt"
)

// Kind 是 JSON 值的类型。
type Kind uint8

const (
	KindInvalid Kind = iota
	KindObject
	KindArray
	KindString
	KindNumber
	KindBool
	KindNull
)

func (k Kind) String() string {
	switch k {
	case KindObject:
		return "object"
	case KindArray:
		return "array"
	case KindString:
		return "string"
	case KindNumber:
		return "number"
	case KindBool:
		return "bool"
	case KindNull:
		return "null"
	default:
		return "invalid"
	}
}

var (
	ErrUnexpectedEnd = errors.New("jsonwalk: unexpected end of input")
	ErrSyntax        = errors.New("jsonwalk: syntax error")
	ErrNotObject     = errors.New("jsonwalk: not an object")
	ErrNotArray      = errors.New("jsonwalk: not an array")
	ErrNotString     = errors.New("jsonwalk: not a string")
	ErrNotNumber     = errors.New("jsonwalk: not a number")
	ErrKeyNotFound   = errors.New("jsonwalk: key not found")
	ErrIndexRange    = errors.New("jsonwalk: index out of range")
)

// Value 描述一个 JSON 值在原始字节中的位置。
//
// Start/End 是半开区间 [Start, End)，指向 src 的切片。
// 字符串值的区间**包含两侧引号**；Raw 是原始字节，永不重新序列化。
type Value struct {
	Kind  Kind
	Start int
	End   int
	Raw   []byte // 指向调用方传入的 src，只读

	// 仅 KindObject：按出现顺序排列的成员
	keys []member
	// 仅 KindArray：按顺序排列的元素
	elems []*Value
}

// member 是对象的单个键值对。重复 key 全部保留，查找时取最后一个。
type member struct {
	keyStart, keyEnd int // 含引号
	val              *Value
}

// Parse 解析 src 并返回根值。src 必须是**完整**的一个 JSON 值。
func Parse(src []byte) (*Value, error) {
	p := &parser{src: src}
	p.skipWS()
	if p.pos >= len(src) {
		return nil, ErrUnexpectedEnd
	}
	v, err := p.parseValue(0)
	if err != nil {
		return nil, err
	}
	p.skipWS()
	if p.pos != len(src) {
		return nil, fmt.Errorf("%w: trailing data at offset %d", ErrSyntax, p.pos)
	}
	return v, nil
}

const maxDepth = 200

type parser struct {
	src []byte
	pos int
}

func (p *parser) skipWS() {
	for p.pos < len(p.src) {
		switch p.src[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *parser) parseValue(depth int) (*Value, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("%w: nesting too deep", ErrSyntax)
	}
	if p.pos >= len(p.src) {
		return nil, ErrUnexpectedEnd
	}
	switch c := p.src[p.pos]; {
	case c == '{':
		return p.parseObject(depth)
	case c == '[':
		return p.parseArray(depth)
	case c == '"':
		return p.parseString()
	case c == 't':
		return p.parseLiteral("true", KindBool)
	case c == 'f':
		return p.parseLiteral("false", KindBool)
	case c == 'n':
		return p.parseLiteral("null", KindNull)
	case c == '-' || (c >= '0' && c <= '9'):
		return p.parseNumber()
	default:
		return nil, fmt.Errorf("%w: unexpected byte %q at offset %d", ErrSyntax, c, p.pos)
	}
}

func (p *parser) parseLiteral(lit string, k Kind) (*Value, error) {
	if p.pos+len(lit) > len(p.src) || string(p.src[p.pos:p.pos+len(lit)]) != lit {
		return nil, fmt.Errorf("%w: bad literal at offset %d", ErrSyntax, p.pos)
	}
	start := p.pos
	p.pos += len(lit)
	return &Value{Kind: k, Start: start, End: p.pos, Raw: p.src[start:p.pos]}, nil
}

func (p *parser) parseObject(depth int) (*Value, error) {
	start := p.pos
	p.pos++ // consume '{'
	v := &Value{Kind: KindObject, Start: start}
	p.skipWS()
	if p.pos < len(p.src) && p.src[p.pos] == '}' {
		p.pos++
		v.End = p.pos
		v.Raw = p.src[start:p.pos]
		return v, nil
	}
	for {
		p.skipWS()
		if p.pos >= len(p.src) {
			return nil, ErrUnexpectedEnd
		}
		if p.src[p.pos] != '"' {
			return nil, fmt.Errorf("%w: expected key at offset %d", ErrSyntax, p.pos)
		}
		keyStart := p.pos
		kv, err := p.parseString()
		if err != nil {
			return nil, err
		}
		keyEnd := kv.End

		p.skipWS()
		if p.pos >= len(p.src) || p.src[p.pos] != ':' {
			return nil, fmt.Errorf("%w: expected ':' at offset %d", ErrSyntax, p.pos)
		}
		p.pos++
		p.skipWS()

		val, err := p.parseValue(depth + 1)
		if err != nil {
			return nil, err
		}
		v.keys = append(v.keys, member{keyStart: keyStart, keyEnd: keyEnd, val: val})

		p.skipWS()
		if p.pos >= len(p.src) {
			return nil, ErrUnexpectedEnd
		}
		switch p.src[p.pos] {
		case ',':
			p.pos++
		case '}':
			p.pos++
			v.End = p.pos
			v.Raw = p.src[start:p.pos]
			return v, nil
		default:
			return nil, fmt.Errorf("%w: expected ',' or '}' at offset %d", ErrSyntax, p.pos)
		}
	}
}

func (p *parser) parseArray(depth int) (*Value, error) {
	start := p.pos
	p.pos++ // consume '['
	v := &Value{Kind: KindArray, Start: start}
	p.skipWS()
	if p.pos < len(p.src) && p.src[p.pos] == ']' {
		p.pos++
		v.End = p.pos
		v.Raw = p.src[start:p.pos]
		return v, nil
	}
	for {
		p.skipWS()
		el, err := p.parseValue(depth + 1)
		if err != nil {
			return nil, err
		}
		v.elems = append(v.elems, el)
		p.skipWS()
		if p.pos >= len(p.src) {
			return nil, ErrUnexpectedEnd
		}
		switch p.src[p.pos] {
		case ',':
			p.pos++
		case ']':
			p.pos++
			v.End = p.pos
			v.Raw = p.src[start:p.pos]
			return v, nil
		default:
			return nil, fmt.Errorf("%w: expected ',' or ']' at offset %d", ErrSyntax, p.pos)
		}
	}
}

// parseString 解析一个字符串，返回区间含引号。
// 只做结构校验（转义合法、UTF-8 边界正确），不构建解码结果。
func (p *parser) parseString() (*Value, error) {
	start := p.pos
	p.pos++ // consume '"'
	for {
		if p.pos >= len(p.src) {
			return nil, ErrUnexpectedEnd
		}
		switch c := p.src[p.pos]; {
		case c == '"':
			p.pos++
			return &Value{Kind: KindString, Start: start, End: p.pos, Raw: p.src[start:p.pos]}, nil
		case c == '\\':
			p.pos++
			if p.pos >= len(p.src) {
				return nil, ErrUnexpectedEnd
			}
			switch e := p.src[p.pos]; e {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				p.pos++
			case 'u':
				p.pos++
				if p.pos+4 > len(p.src) {
					return nil, ErrUnexpectedEnd
				}
				for i := 0; i < 4; i++ {
					if !isHex(p.src[p.pos+i]) {
						return nil, fmt.Errorf("%w: bad \\u escape at offset %d", ErrSyntax, p.pos)
					}
				}
				p.pos += 4
			default:
				return nil, fmt.Errorf("%w: bad escape \\%c at offset %d", ErrSyntax, e, p.pos)
			}
		case c < 0x20:
			return nil, fmt.Errorf("%w: raw control byte 0x%02x in string at offset %d", ErrSyntax, c, p.pos)
		default:
			p.pos++
		}
	}
}

func isHex(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

// parseNumber 只做语法校验，保留字面量。
// 关键：**不转换成 float64** —— 大整数会失真（PLAN.md §5.1）。
//
// 按 RFC 8259 的 number 文法校验：-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?
// 特别注意前导零：JSON 禁止 01 / -01（旧版 C++ 用 strtoull 会接受，我们不接受）。
func (p *parser) parseNumber() (*Value, error) {
	start := p.pos
	if p.pos < len(p.src) && p.src[p.pos] == '-' {
		p.pos++
	}
	if p.pos >= len(p.src) {
		return nil, ErrUnexpectedEnd
	}

	// 整数部分：单个 0，或 [1-9] 后跟任意数字
	if p.src[p.pos] == '0' {
		p.pos++
		if p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
			return nil, fmt.Errorf("%w: leading zero at offset %d", ErrSyntax, start)
		}
	} else if p.src[p.pos] >= '1' && p.src[p.pos] <= '9' {
		for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
			p.pos++
		}
	} else {
		return nil, fmt.Errorf("%w: bad number at offset %d", ErrSyntax, p.pos)
	}

	if p.pos < len(p.src) && p.src[p.pos] == '.' {
		p.pos++
		frac := 0
		for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
			p.pos++
			frac++
		}
		if frac == 0 {
			return nil, fmt.Errorf("%w: bad fraction at offset %d", ErrSyntax, p.pos)
		}
	}
	if p.pos < len(p.src) && (p.src[p.pos] == 'e' || p.src[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.src) && (p.src[p.pos] == '+' || p.src[p.pos] == '-') {
			p.pos++
		}
		exp := 0
		for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
			p.pos++
			exp++
		}
		if exp == 0 {
			return nil, fmt.Errorf("%w: bad exponent at offset %d", ErrSyntax, p.pos)
		}
	}
	return &Value{Kind: KindNumber, Start: start, End: p.pos, Raw: p.src[start:p.pos]}, nil
}
