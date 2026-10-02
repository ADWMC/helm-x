package jsonwalk

import "fmt"

// Member 返回对象的直接子成员。重复 key 取**最后一个**（与 JSON 语义一致）。
// 键比较在解码后的文本上进行，因此 "\u0072ole" 与 "role" 视为同一个键。
func (v *Value) Member(key string) *Value {
	if v == nil || v.Kind != KindObject {
		return nil
	}
	var found *Value
	for _, m := range v.keys {
		k, err := unescape(v.Raw[m.keyStart-v.Start : m.keyEnd-v.Start])
		if err != nil {
			continue
		}
		if k == key {
			found = m.val
		}
	}
	return found
}

// Members 按出现顺序返回全部键名（含重复）。
func (v *Value) Members() []string {
	if v == nil || v.Kind != KindObject {
		return nil
	}
	out := make([]string, 0, len(v.keys))
	for _, m := range v.keys {
		k, err := unescape(v.Raw[m.keyStart-v.Start : m.keyEnd-v.Start])
		if err == nil {
			out = append(out, k)
		}
	}
	return out
}

// Index 返回数组的第 i 个元素，越界返回 nil。
func (v *Value) Index(i int) *Value {
	if v == nil || v.Kind != KindArray {
		return nil
	}
	if i < 0 || i >= len(v.elems) {
		return nil
	}
	return v.elems[i]
}

// Len 返回数组元素数或对象成员数；其他类型返回 0。
func (v *Value) Len() int {
	if v == nil {
		return 0
	}
	switch v.Kind {
	case KindArray:
		return len(v.elems)
	case KindObject:
		return len(v.keys)
	default:
		return 0
	}
}

// String 返回解码后的字符串值。非字符串返回 error。
func (v *Value) String() (string, error) {
	if v == nil {
		return "", ErrNotString
	}
	if v.Kind != KindString {
		return "", fmt.Errorf("%w: got %s", ErrNotString, v.Kind)
	}
	return unescape(v.Raw)
}

// MustString 是 String 的便捷包装，失败返回空串。
func (v *Value) MustString() string {
	s, err := v.String()
	if err != nil {
		return ""
	}
	return s
}

// Number 返回数字的**原始字面量**，不经过 float64。
func (v *Value) Number() (string, error) {
	if v == nil || v.Kind != KindNumber {
		return "", ErrNotNumber
	}
	return string(v.Raw), nil
}

// Bool 返回布尔值。
func (v *Value) Bool() (bool, error) {
	if v == nil || v.Kind != KindBool {
		return false, fmt.Errorf("%w: got %v", ErrSyntax, v)
	}
	return string(v.Raw) == "true", nil
}

// Lookup 按路径依次下钻：字符串段取成员，整数段取数组下标。
//
//	v.Lookup("input", 0, "content") 等价于 v.Member("input").Index(0).Member("content")
func (v *Value) Lookup(path ...any) *Value {
	cur := v
	for _, seg := range path {
		if cur == nil {
			return nil
		}
		switch s := seg.(type) {
		case string:
			cur = cur.Member(s)
		case int:
			cur = cur.Index(s)
		default:
			return nil
		}
	}
	return cur
}

// Walk 深度优先遍历自身及所有后代。fn 返回 false 时中止该分支。
// 遍历顺序即文档顺序。
func (v *Value) Walk(fn func(*Value) bool) {
	if v == nil {
		return
	}
	if !fn(v) {
		return
	}
	for _, e := range v.elems {
		e.Walk(fn)
	}
	for _, m := range v.keys {
		m.val.Walk(fn)
	}
}
