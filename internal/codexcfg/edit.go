package codexcfg

import (
	"bytes"
	"fmt"
)

// scopeEquals 判断行的 Scope 是否等于给定表路径。
// nil 与空切片都表示顶层。
func scopeEquals(scope, table []string) bool {
	if len(scope) != len(table) {
		return false
	}
	for i := range scope {
		if scope[i] != table[i] {
			return false
		}
	}
	return true
}

// isTopLevel 判断表路径是否为顶层。
func isTopLevel(table []string) bool { return len(table) == 0 }

// Get 返回值区间的原始字节（不含引号处理，调用方自行解读）。
// 找不到返回 false。
func (d *Doc) Get(table []string, key string) ([]byte, bool) {
	for i := range d.lines {
		l := &d.lines[i]
		if l.Kind != LineKV || l.Key != key {
			continue
		}
		if !scopeEquals(l.Scope, table) {
			continue
		}
		return l.Raw[l.ValueRange[0]:l.ValueRange[1]], true
	}
	return nil, false
}

// GetString 取字符串键的值（去引号）。
func (d *Doc) GetString(table []string, key string) (string, bool) {
	raw, ok := d.Get(table, key)
	if !ok {
		return "", false
	}
	raw = bytes.TrimSpace(raw)
	s, ok2 := unquoteKey(raw)
	if !ok2 {
		return "", false
	}
	return s, true
}

// Has 判断某个键是否存在。
func (d *Doc) Has(table []string, key string) bool {
	_, ok := d.Get(table, key)
	return ok
}

// SetString 设置已存在键的值。只替换值区间，行的其余字节不变。
//
// 返回值是否成功；键不存在时返回 ErrKeyNotFound，
// 此时调用方应改用 InsertTopLevel（这是两件不同的事，不静默合并）。
func (d *Doc) SetString(table []string, key, val string) error {
	for i := range d.lines {
		l := &d.lines[i]
		if l.Kind != LineKV || l.Key != key || !scopeEquals(l.Scope, table) {
			continue
		}
		start, end := l.ValueRange[0], l.ValueRange[1]
		if start > len(l.Raw) || end > len(l.Raw) || start > end {
			return fmt.Errorf("%w: 值区间非法", ErrUnsupportedForm)
		}
		quoted := quoteTOMLString(val)
		newRaw := make([]byte, 0, len(l.Raw)-(end-start)+len(quoted))
		newRaw = append(newRaw, l.Raw[:start]...)
		newRaw = append(newRaw, quoted...)
		newRaw = append(newRaw, l.Raw[end:]...)
		l.Raw = newRaw
		l.ValueRange = [2]int{start, start + len(quoted)}
		return nil
	}
	return fmt.Errorf("%w: %v.%s", ErrKeyNotFound, table, key)
}

// SetRaw 设置已存在键的值，值以原始 TOML 片段给出（如数组、数字、布尔）。
func (d *Doc) SetRaw(table []string, key string, rawVal []byte) error {
	for i := range d.lines {
		l := &d.lines[i]
		if l.Kind != LineKV || l.Key != key || !scopeEquals(l.Scope, table) {
			continue
		}
		start, end := l.ValueRange[0], l.ValueRange[1]
		if start > len(l.Raw) || end > len(l.Raw) || start > end {
			return fmt.Errorf("%w: 值区间非法", ErrUnsupportedForm)
		}
		newRaw := make([]byte, 0, len(l.Raw)-(end-start)+len(rawVal))
		newRaw = append(newRaw, l.Raw[:start]...)
		newRaw = append(newRaw, rawVal...)
		newRaw = append(newRaw, l.Raw[end:]...)
		l.Raw = newRaw
		l.ValueRange = [2]int{start, start + len(rawVal)}
		return nil
	}
	return fmt.Errorf("%w: %v.%s", ErrKeyNotFound, table, key)
}

// InsertTopLevel 在文档头部的顶层区域插入 `key = value`（字符串值）。
// 已存在则不插入（幂等）。
//
// 插入位置：第一个非注释、非空行之前 —— 保持文件头部的键聚集，
// 且不会误插到某个表内部。
func (d *Doc) InsertTopLevel(key, val string) error {
	if d.Has(nil, key) {
		return nil
	}
	pos := d.topLevelInsertPos()
	line := []byte(key + " = " + quoteTOMLString(val) + d.eol)
	inserted, err := classify(line)
	if err != nil {
		return err
	}
	inserted.Scope = nil

	rest := append([]Line(nil), d.lines[pos:]...)
	d.lines = append(d.lines[:pos], inserted)
	d.lines = append(d.lines, rest...)
	return nil
}

// InsertTopLevelRaw 与 InsertTopLevel 相同，但值以原始 TOML 片段给出。
func (d *Doc) InsertTopLevelRaw(key string, rawVal []byte) error {
	if d.Has(nil, key) {
		return nil
	}
	pos := d.topLevelInsertPos()
	line := make([]byte, 0, len(key)+len(rawVal)+8)
	line = append(line, key+" = "...)
	line = append(line, rawVal...)
	line = append(line, d.eol...)
	inserted, err := classify(line)
	if err != nil {
		return err
	}
	inserted.Scope = nil

	rest := append([]Line(nil), d.lines[pos:]...)
	d.lines = append(d.lines[:pos], inserted)
	d.lines = append(d.lines, rest...)
	return nil
}

// topLevelInsertPos 返回顶层插入点：跳过开头的注释与空行，
// 但停在第一个表头之前。若文件以表头开头，则插到表头之前。
func (d *Doc) topLevelInsertPos() int {
	for i := range d.lines {
		if d.lines[i].Kind == LineTable {
			return i
		}
	}
	// 全是顶层键与注释：插到第一条键行处；没有键行则插到末尾（在末行换行修正后）
	for i := range d.lines {
		if d.lines[i].Kind == LineKV {
			return i
		}
	}
	return len(d.lines)
}

// Tables 列出文档中出现的全部表路径，按出现顺序。
func (d *Doc) Tables() [][]string {
	var out [][]string
	for i := range d.lines {
		if d.lines[i].Kind == LineTable {
			out = append(out, d.lines[i].Table)
		}
	}
	return out
}

// HasTable 判断某个表是否存在。
func (d *Doc) HasTable(table []string) bool {
	for i := range d.lines {
		if d.lines[i].Kind == LineTable && scopeEquals(d.lines[i].Table, table) {
			return true
		}
	}
	return false
}

// quoteTOMLString 把字符串编码为 TOML 基本字符串（含两侧引号）。
func quoteTOMLString(s string) string {
	var sb bytes.Buffer
	sb.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
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
		default:
			if c < 0x20 {
				fmt.Fprintf(&sb, `\u%04X`, c)
			} else {
				sb.WriteByte(c)
			}
		}
	}
	sb.WriteByte('"')
	return sb.String()
}
