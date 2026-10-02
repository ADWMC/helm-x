package codexcfg

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrHomeNotFound     = errors.New("codexcfg: 未找到 codex 配置目录")
	ErrConfigNotFound   = errors.New("codexcfg: 未找到 config.toml")
	ErrUnsupportedForm  = errors.New("codexcfg: 配置包含本包不支持的 TOML 形态")
	ErrKeyNotFound      = errors.New("codexcfg: 未找到指定的键")
	ErrTableNotFound    = errors.New("codexcfg: 未找到指定的表")
	ErrNoBackup         = errors.New("codexcfg: 没有可用的完整备份")
	ErrNoProxyBackup    = errors.New("codexcfg: 没有 base_url 还原点")
	ErrNoActiveProvider = errors.New("codexcfg: 配置中没有激活的 provider")
)

// LineKind 是行级模型的分类。只识别结构，不解析值语义。
type LineKind uint8

const (
	LineBlank   LineKind = iota // 空行或纯空白
	LineComment                 // 以 # 开头的行（允许前导空白）
	LineTable                   // [table] 或 [a.b.c]
	LineKV                      // key = value
	LineOther                   // 无法归类（如续行）—— 视为不支持
)

// Line 是文档中的一行。
//
// Raw 含行尾换行符（若有）。改值时只替换 ValueRange 区间，
// 行内其他字节（缩进、键名、等号周围的空格）保持不变。
type Line struct {
	Kind LineKind
	Raw  []byte

	// 仅 LineTable：表名（已去引号、已展开点分）
	Table []string

	// 仅 LineKV
	Key        string // 已去引号
	KeyRange   [2]int // Key 在 Raw 中的 [start,end)
	ValueRange [2]int // 值在 Raw 中的 [start,end)，不含行尾换行

	// 该行所处的表路径（"" 表示顶层）。键行与表头行都记录，
	// 便于判断某个键属于哪个表。
	Scope []string
}

// Doc 是行级文档模型。
//
// 保持原始行的切片，任何修改都通过重写单行的 Raw 完成，
// 未触碰的行逐字节保留（INV-6）。
type Doc struct {
	lines []Line
	eol   string // "\n" 或 "\r\n"
	// hadFinalNewline 记录原文件末尾是否有换行，写回时保持一致
	hadFinalNewline bool
}

// ParseDoc 解析原始字节为行级文档。
//
// 检测到不支持的形态（多行字符串、[[array]]）时返回 ErrUnsupportedForm，
// 调用方应拒绝写入而不是猜测（ADR-001）。
func ParseDoc(raw []byte) (*Doc, error) {
	d := &Doc{eol: detectEOL(raw), hadFinalNewline: len(raw) > 0 && raw[len(raw)-1] == '\n'}

	// 按行切分，保留每行的换行符
	rest := raw
	for len(rest) > 0 {
		idx := bytes.IndexByte(rest, '\n')
		var rawLine []byte
		if idx < 0 {
			rawLine = rest
			rest = nil
		} else {
			rawLine = rest[:idx+1]
			rest = rest[idx+1:]
		}
		ln, err := classify(rawLine)
		if err != nil {
			return nil, err
		}
		d.lines = append(d.lines, ln)
	}

	// 第二遍：填充每行的 Scope
	scope := []string(nil)
	for i := range d.lines {
		switch d.lines[i].Kind {
		case LineTable:
			scope = d.lines[i].Table
			d.lines[i].Scope = append([]string(nil), scope...)
		case LineKV:
			d.lines[i].Scope = append([]string(nil), scope...)
		}
	}
	return d, nil
}

// Bytes 序列化回字节。未修改的行与输入逐字节一致。
func (d *Doc) Bytes() []byte {
	var buf bytes.Buffer
	for _, l := range d.lines {
		buf.Write(l.Raw)
	}
	return buf.Bytes()
}

// EOL 返回探测到的行尾风格。
func (d *Doc) EOL() string { return d.eol }

// Lines 返回只读的行视图（供测试与诊断）。
func (d *Doc) Lines() []Line { return d.lines }

// classify 判断单行的类别。
func classify(raw []byte) (Line, error) {
	ln := Line{Raw: raw}
	body := trimEOL(raw)
	trimmed := bytes.TrimLeft(body, " \t")

	if len(trimmed) == 0 {
		ln.Kind = LineBlank
		return ln, nil
	}
	if trimmed[0] == '#' {
		ln.Kind = LineComment
		return ln, nil
	}

	// 不支持的形态：检测到即拒绝（ADR-001）
	if bytes.HasPrefix(trimmed, []byte("[[")) {
		return ln, fmt.Errorf("%w: [[array of tables]]", ErrUnsupportedForm)
	}
	if bytes.Contains(body, []byte(`"""`)) || bytes.Contains(body, []byte("'''")) {
		return ln, fmt.Errorf("%w: 多行字符串", ErrUnsupportedForm)
	}

	if trimmed[0] == '[' {
		table, ok := parseTableHeader(trimmed)
		if !ok {
			return ln, fmt.Errorf("%w: 无法解析的表头 %q", ErrUnsupportedForm, trimmed)
		}
		ln.Kind = LineTable
		ln.Table = table
		return ln, nil
	}

	// key = value
	eq := bytes.IndexByte(trimmed, '=')
	if eq <= 0 {
		// 非注释、非表头、无等号：视为续行或异常，拒绝
		return ln, fmt.Errorf("%w: 无法解析的行 %q", ErrUnsupportedForm, trimmed)
	}
	keyRaw := bytes.TrimRight(trimmed[:eq], " \t")
	key, ok := unquoteKey(keyRaw)
	if !ok {
		return ln, fmt.Errorf("%w: 无法解析的键 %q", ErrUnsupportedForm, keyRaw)
	}

	// 计算 Key / Value 的区间。
	//
	// 注意：body 是 raw 去掉行尾换行后的切片，两者**前缀相同**，
	// 因此 body 上的偏移可直接用于 raw —— 只要不越过 body 的末尾。
	lead := len(body) - len(trimmed) // 前导空白长度
	keyStart := lead
	keyEnd := keyStart + len(keyRaw)

	valStart := lead + eq + 1
	for valStart < len(body) && (body[valStart] == ' ' || body[valStart] == '\t') {
		valStart++
	}
	// 行尾注释：找到未被引号包裹的 '#' 作为值结束点。
	valEnd := stripInlineComment(body, valStart)

	ln.Kind = LineKV
	ln.Key = key
	ln.KeyRange = [2]int{keyStart, keyEnd}
	ln.ValueRange = [2]int{valStart, valEnd}
	return ln, nil
}

func trimEOL(b []byte) []byte {
	return bytes.TrimRight(b, "\r\n")
}

// parseTableHeader 解析 [a.b.c] 或 [a."b.c"]。
func parseTableHeader(trimmed []byte) ([]string, bool) {
	end := bytes.LastIndexByte(trimmed, ']')
	if end < 0 {
		return nil, false
	}
	inner := trimmed[1:end]
	// 去掉尾随注释
	if h := bytes.IndexByte(inner, '#'); h >= 0 {
		inner = bytes.TrimRight(inner[:h], " \t")
	}
	if len(inner) == 0 {
		return nil, false
	}
	return splitDottedKey(inner)
}

// splitDottedKey 按点切分，正确处理带引号的段。
// `plugins."visualize@openai-bundled"` → ["plugins", "visualize@openai-bundled"]
func splitDottedKey(b []byte) ([]string, bool) {
	var parts []string
	var cur []byte
	inQuote := byte(0)

	flush := func() bool {
		seg := bytes.TrimSpace(cur)
		if len(seg) == 0 {
			return false
		}
		s, ok := unquoteKey(seg)
		if !ok {
			return false
		}
		parts = append(parts, s)
		cur = nil
		return true
	}

	for i := 0; i < len(b); i++ {
		c := b[i]
		switch {
		case inQuote != 0:
			cur = append(cur, c)
			if c == inQuote {
				inQuote = 0
			}
		case c == '"' || c == '\'':
			inQuote = c
			cur = append(cur, c)
		case c == '.':
			if !flush() {
				return nil, false
			}
		default:
			cur = append(cur, c)
		}
	}
	if inQuote != 0 {
		return nil, false
	}
	if !flush() {
		return nil, false
	}
	return parts, true
}

// unquoteKey 去掉键两侧的引号。
func unquoteKey(b []byte) (string, bool) {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return "", false
	}
	if b[0] == '"' || b[0] == '\'' {
		q := b[0]
		if len(b) < 2 || b[len(b)-1] != q {
			return "", false
		}
		inner := b[1 : len(b)-1]
		// 基本转义处理（键名里罕见，但 spec 允许）
		if q == '"' && bytes.ContainsRune(inner, '\\') {
			s, err := unescapeBasic(string(inner))
			if err != nil {
				return "", false
			}
			return s, true
		}
		return string(inner), true
	}
	return string(b), true
}

func unescapeBasic(s string) (string, error) {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			sb.WriteByte(s[i])
			continue
		}
		i++
		if i >= len(s) {
			return "", errors.New("trailing backslash")
		}
		switch s[i] {
		case 'n':
			sb.WriteByte('\n')
		case 't':
			sb.WriteByte('\t')
		case 'r':
			sb.WriteByte('\r')
		case '"':
			sb.WriteByte('"')
		case '\\':
			sb.WriteByte('\\')
		default:
			sb.WriteByte(s[i])
		}
	}
	return sb.String(), nil
}

// stripInlineComment 返回值的结束位置，排除行尾注释。
//
// 规则：只有在引号已闭合、且 '#' 之前的最后一个非空白字符之后才可能是注释。
// 这样 `model = "a#b"` 与 `key = value # note` 都能正确处理。
//
// 注意初值：lastNonWS 从 valStart 开始，表示「目前已知值的长度至少为 0」。
// 循环中每消费一个字符就推进 —— 引号字符本身也算值的一部分。
func stripInlineComment(body []byte, valStart int) int {
	inQuote := byte(0)
	lastNonWS := valStart
	for i := valStart; i < len(body); i++ {
		c := body[i]
		if inQuote != 0 {
			lastNonWS = i + 1 // 消费该字符（含结束引号）
			if c == '\\' && inQuote == '"' {
				if i+1 < len(body) {
					lastNonWS = i + 2 // 跳过被转义的字符
				}
				i++
				continue
			}
			if c == inQuote {
				inQuote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			inQuote = c
			lastNonWS = i + 1
		case '#':
			return lastNonWS
		case ' ', '\t':
			// 空白不计入，但也不结束；若后面还有非空白会继续推进
		default:
			lastNonWS = i + 1
		}
	}
	return len(body)
}
