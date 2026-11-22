package proxy

import "testing"

// instructionsSource：这一轮系统提示词到底是谁写的（纯观测字段）。
func TestInstructionsSource(t *testing.T) {
	cases := []struct {
		injected bool
		deploy   string
		client   bool
		want     string
	}{
		{true, "replace-base", true, "ours"},  // 覆盖掉客户端文本
		{true, "replace-base", false, "ours"}, // 本来就没有
		{true, "insert", true, "ours+client"}, // 叠加
		{true, "append", true, "ours+client"}, // 叠加
		{true, "insert", false, "ours"},       // 只有我们
		{false, "", true, "client"},           // 没注入，客户端自带
		{false, "", false, ""},                // 谁都没写
	}
	for _, c := range cases {
		if got := instructionsSource(c.injected, c.deploy, c.client); got != c.want {
			t.Errorf("instructionsSource(%v,%q,%v) = %q, want %q", c.injected, c.deploy, c.client, got, c.want)
		}
	}
}
