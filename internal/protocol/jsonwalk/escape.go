package jsonwalk

import (
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// unescape 解码一个含引号的 JSON 字符串字面量。
// 支持 \uXXXX 及代理对（PLAN.md §5.1 要求）。
func unescape(raw []byte) (string, error) {
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return "", fmt.Errorf("%w: not a quoted string", ErrSyntax)
	}
	body := raw[1 : len(raw)-1]

	// 快路径：无转义直接返回
	if !strings.ContainsRune(string(body), '\\') {
		return string(body), nil
	}

	var sb strings.Builder
	sb.Grow(len(body))
	for i := 0; i < len(body); {
		c := body[i]
		if c != '\\' {
			sb.WriteByte(c)
			i++
			continue
		}
		if i+1 >= len(body) {
			return "", ErrUnexpectedEnd
		}
		switch e := body[i+1]; e {
		case '"':
			sb.WriteByte('"')
			i += 2
		case '\\':
			sb.WriteByte('\\')
			i += 2
		case '/':
			sb.WriteByte('/')
			i += 2
		case 'b':
			sb.WriteByte('\b')
			i += 2
		case 'f':
			sb.WriteByte('\f')
			i += 2
		case 'n':
			sb.WriteByte('\n')
			i += 2
		case 'r':
			sb.WriteByte('\r')
			i += 2
		case 't':
			sb.WriteByte('\t')
			i += 2
		case 'u':
			r1, n, err := decodeHex4(body[i+2:])
			if err != nil {
				return "", err
			}
			i += 2 + n
			// 代理对：高位 0xD800-0xDBFF 需要后跟低位 0xDC00-0xDFFF
			if utf16.IsSurrogate(rune(r1)) && i+6 <= len(body) &&
				body[i] == '\\' && body[i+1] == 'u' {
				r2, n2, err := decodeHex4(body[i+2:])
				if err == nil {
					combined := utf16.DecodeRune(rune(r1), rune(r2))
					if combined != utf8.RuneError {
						sb.WriteRune(combined)
						i += 2 + n2
						continue
					}
				}
			}
			sb.WriteRune(rune(r1))
		default:
			return "", fmt.Errorf("%w: bad escape \\%c", ErrSyntax, e)
		}
	}
	return sb.String(), nil
}

func decodeHex4(b []byte) (uint32, int, error) {
	if len(b) < 4 {
		return 0, 0, ErrUnexpectedEnd
	}
	var v uint32
	for i := 0; i < 4; i++ {
		c := b[i]
		var d uint32
		switch {
		case c >= '0' && c <= '9':
			d = uint32(c - '0')
		case c >= 'a' && c <= 'f':
			d = uint32(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = uint32(c-'A') + 10
		default:
			return 0, 0, fmt.Errorf("%w: bad hex digit %q", ErrSyntax, c)
		}
		v = v<<4 | d
	}
	return v, 4, nil
}

// escapeString 把明文编码为不含两侧引号的 JSON 字符串体。
// 只转义 JSON 必需的最少字符，保持输出可读。
func escapeString(s string) string {
	var sb strings.Builder
	sb.Grow(len(s) + 8)
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		case '\b':
			sb.WriteString(`\b`)
		case '\f':
			sb.WriteString(`\f`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&sb, `\u%04x`, r)
			} else {
				sb.WriteRune(r)
			}
		}
	}
	return sb.String()
}
