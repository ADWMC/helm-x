package jsonwalk

import "fmt"

// 本文件实现三个改写操作。所有操作都基于原始字节做区间替换，
// **不重新序列化整个文档** —— 这是保真的关键（PLAN.md §5.1）。
//
// 每个操作返回新的 []byte，输入不被修改。区间按**从后往前**的顺序应用，
// 避免前面的替换使后面的偏移失效。

// ReplaceString 把字符串值替换为 val，自动处理转义。
// 仅对 KindString 有效。返回替换后的完整文档。
func ReplaceString(src []byte, v *Value, val string) ([]byte, error) {
	if v == nil {
		return nil, fmt.Errorf("%w: nil value", ErrSyntax)
	}
	if v.Kind != KindString {
		return nil, fmt.Errorf("%w: got %s", ErrNotString, v.Kind)
	}
	if v.Start < 0 || v.End > len(src) || v.Start > v.End {
		return nil, fmt.Errorf("%w: value range [%d,%d) outside src(%d)", ErrSyntax, v.Start, v.End, len(src))
	}
	repl := `"` + escapeString(val) + `"`
	out := make([]byte, 0, len(src)-(v.End-v.Start)+len(repl))
	out = append(out, src[:v.Start]...)
	out = append(out, repl...)
	out = append(out, src[v.End:]...)
	return out, nil
}

// ReplaceRaw 用一个原始 JSON 片段替换该值的区间。
// raw 必须是合法的 JSON 值；本函数不做校验，调用方负责。
func ReplaceRaw(src []byte, v *Value, raw []byte) ([]byte, error) {
	if v == nil {
		return nil, fmt.Errorf("%w: nil value", ErrSyntax)
	}
	if v.Start < 0 || v.End > len(src) || v.Start > v.End {
		return nil, fmt.Errorf("%w: value range [%d,%d) outside src(%d)", ErrSyntax, v.Start, v.End, len(src))
	}
	out := make([]byte, 0, len(src)-(v.End-v.Start)+len(raw))
	out = append(out, src[:v.Start]...)
	out = append(out, raw...)
	out = append(out, src[v.End:]...)
	return out, nil
}

// InsertFirst 把 raw 作为数组的第一个元素（或对象的第一个成员）插入。
//
// raw 必须自带结尾逗号的处理：本函数负责补分隔符。
// 数组：插入到 '[' 之后，若原有元素则补逗号。
// 对象：raw 需形如 `"key":value`，插入到 '{' 之后。
func InsertFirst(src []byte, v *Value, raw []byte) ([]byte, error) {
	if v == nil {
		return nil, fmt.Errorf("%w: nil value", ErrSyntax)
	}
	if v.Kind != KindArray && v.Kind != KindObject {
		return nil, fmt.Errorf("%w: cannot insert into %s", ErrSyntax, v.Kind)
	}
	if v.Start >= len(src) {
		return nil, fmt.Errorf("%w: value start outside src", ErrSyntax)
	}

	open := v.Start      // 指向 '[' 或 '{'
	insertAt := open + 1 // 紧跟其后
	empty := v.Len() == 0

	var payload []byte
	if empty {
		payload = append(payload, raw...)
	} else {
		payload = make([]byte, 0, len(raw)+1)
		payload = append(payload, raw...)
		payload = append(payload, ',')
	}

	out := make([]byte, 0, len(src)+len(payload))
	out = append(out, src[:insertAt]...)
	out = append(out, payload...)
	out = append(out, src[insertAt:]...)
	return out, nil
}

// AppendLast 把 raw 作为数组最后一个元素（或对象最后一个成员）插入。
func AppendLast(src []byte, v *Value, raw []byte) ([]byte, error) {
	if v == nil {
		return nil, fmt.Errorf("%w: nil value", ErrSyntax)
	}
	if v.Kind != KindArray && v.Kind != KindObject {
		return nil, fmt.Errorf("%w: cannot append to %s", ErrSyntax, v.Kind)
	}
	// End 指向闭合的 ']' 或 '}'
	closeAt := v.End - 1
	if closeAt < v.Start || closeAt >= len(src) {
		return nil, fmt.Errorf("%w: bad value range", ErrSyntax)
	}

	var payload []byte
	if v.Len() == 0 {
		payload = append(payload, raw...)
	} else {
		payload = append(payload, ',')
		payload = append(payload, raw...)
	}

	out := make([]byte, 0, len(src)+len(payload))
	out = append(out, src[:closeAt]...)
	out = append(out, payload...)
	out = append(out, src[closeAt:]...)
	return out, nil
}

// Delete 删除该值及其相邻的一个分隔符，保持文档合法。
func Delete(src []byte, v *Value) ([]byte, error) {
	if v == nil {
		return nil, fmt.Errorf("%w: nil value", ErrSyntax)
	}
	start, end := v.Start, v.End
	if start < 0 || end > len(src) || start > end {
		return nil, fmt.Errorf("%w: bad range", ErrSyntax)
	}

	// 优先吃掉后面的逗号与空白；没有则吃前面的
	i := end
	for i < len(src) && isWS(src[i]) {
		i++
	}
	if i < len(src) && src[i] == ',' {
		i++
		for i < len(src) && isWS(src[i]) {
			i++
		}
		out := make([]byte, 0, len(src)-(i-start))
		out = append(out, src[:start]...)
		out = append(out, src[i:]...)
		return out, nil
	}

	j := start
	for j > 0 && isWS(src[j-1]) {
		j--
	}
	if j > 0 && src[j-1] == ',' {
		j--
		for j > 0 && isWS(src[j-1]) {
			j--
		}
		out := make([]byte, 0, len(src)-(end-j))
		out = append(out, src[:j]...)
		out = append(out, src[end:]...)
		return out, nil
	}

	// 既无前置也无后置逗号：单元素容器，直接删值
	out := make([]byte, 0, len(src)-(end-start))
	out = append(out, src[:start]...)
	out = append(out, src[end:]...)
	return out, nil
}

// Splice 是通用区间替换：把 src[start:end) 换成 raw。
// 供需要自定义改写的调用方使用。多个 Splice 必须按 start **降序**调用。
func Splice(src []byte, start, end int, raw []byte) ([]byte, error) {
	if start < 0 || end > len(src) || start > end {
		return nil, fmt.Errorf("%w: splice range [%d,%d) outside src(%d)", ErrSyntax, start, end, len(src))
	}
	out := make([]byte, 0, len(src)-(end-start)+len(raw))
	out = append(out, src[:start]...)
	out = append(out, raw...)
	out = append(out, src[end:]...)
	return out, nil
}

func isWS(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
