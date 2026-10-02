package codexcfg

import (
	"fmt"
	"strings"
)

// 本文件只做**只读**探测：回答"当前配置是什么状态"。
// 不写入、不备份、不改文件。供 UI 展示与代理启动时读取上游地址。

// ActiveProvider 描述当前激活的 provider。
type ActiveProvider struct {
	// Name 是顶层 model_provider 的值，如 "1145"。
	Name string
	// Table 是它在 [model_providers.X] 中的表路径。
	Table []string
	// BaseURL 是该 provider 当前的 base_url。
	BaseURL string
	// WireAPI 是 wire_api 的值（responses / chat_completions），可能为空。
	WireAPI string
}

// IsLocalProxy 报告 base_url 是否指向本地。
// 用于判断"是否处于代理态"，以及避免把代理地址存进还原点。
func (p ActiveProvider) IsLocalProxy() bool {
	u := p.BaseURL
	return strings.Contains(u, "127.0.0.1") || strings.Contains(u, "localhost") || strings.Contains(u, "[::1]")
}

// Probe 读取当前激活的 provider 及其 base_url。
//
// 与旧版 read_active_provider（config.cpp:474-483）等价，
// 但表路径由行级模型解析，不依赖字符串拼接表头。
func (h Home) Probe() (ActiveProvider, error) {
	d, _, err := h.ParseConfig()
	if err != nil {
		return ActiveProvider{}, err
	}

	name, ok := d.GetString(nil, "model_provider")
	if !ok || name == "" {
		return ActiveProvider{}, ErrNoActiveProvider
	}

	ap := ActiveProvider{
		Name:  name,
		Table: []string{"model_providers", name},
	}
	if !d.HasTable(ap.Table) {
		// provider 声明缺失：仍返回名字，让调用方给出可读错误
		return ap, fmt.Errorf("%w: [model_providers.%s] 不存在", ErrNoActiveProvider, name)
	}
	if u, ok := d.GetString(ap.Table, "base_url"); ok {
		ap.BaseURL = u
	}
	if w, ok := d.GetString(ap.Table, "wire_api"); ok {
		ap.WireAPI = w
	}
	return ap, nil
}

// RelayURL 返回"真实上游"地址。
//
// 语义（与旧版 read_relay_url 一致，config.cpp:452-472）：
//   - 若 base_url 不指向本地，它本身就是上游
//   - 若指向本地（代理态），则从还原点取原始地址
//
// 返回空串表示无法确定 —— 调用方应提示"config 里没有 base_url"。
func (h Home) RelayURL() (string, error) {
	ap, err := h.Probe()
	if err != nil {
		return "", err
	}
	if !ap.IsLocalProxy() {
		return ap.BaseURL, nil
	}

	raw, err := h.ReadProxyBak()
	if err != nil {
		return "", nil // 代理态但没有还原点：无法确定，返回空而非报错
	}
	bd, err := ParseDoc(raw)
	if err != nil {
		return "", fmt.Errorf("还原点不可解析: %w", err)
	}
	orig, ok := bd.GetString(ap.Table, "base_url")
	if !ok || orig == "" {
		return "", nil
	}
	if strings.Contains(orig, "127.0.0.1") {
		return "", nil // 还原点里也是本地地址，无效
	}
	return orig, nil
}

// InjectionState 描述注入状态，供自检与 UI 使用。
type InjectionState struct {
	Home           string
	ConfigExists   bool
	Injected       bool
	HasBackup      bool
	HasProxyBackup bool
	Provider       string
	BaseURL        string
	MissingKeys    []string
}

// Proxied 报告 base_url 是否已指向本地代理。
//
// 用于界面显示"codex 是否正走本代理"，以及判断退出时是否需要还原。
func (st InjectionState) Proxied() bool {
	u := st.BaseURL
	return strings.Contains(u, "127.0.0.1") ||
		strings.Contains(u, "localhost") ||
		strings.Contains(u, "[::1]")
}

// 注入时写入的三个上下文默认键（与旧版 config.cpp:317-327 一致）。
var ContextDefaultKeys = []struct {
	Key string
	Val string // 原始 TOML 片段
}{
	{"tool_output_token_limit", "8000"},
	{"model_auto_compact_token_limit", "180000"},
	{"model_auto_compact_token_limit_scope", `"body_after_prefix"`},
}

// CheckInjection 只读地报告注入状态。
//
// 判定条件与旧版 verify_injection（config.cpp:485-497）一致：
// 三个上下文键**全部存在**才算已注入。
func (h Home) CheckInjection() InjectionState {
	st := InjectionState{
		Home:           h.Dir,
		HasBackup:      h.HasBackup(),
		HasProxyBackup: h.HasProxyBackup(),
	}

	d, _, err := h.ParseConfig()
	if err != nil {
		return st
	}
	st.ConfigExists = true

	for _, kv := range ContextDefaultKeys {
		if !d.Has(nil, kv.Key) {
			st.MissingKeys = append(st.MissingKeys, kv.Key)
		}
	}
	st.Injected = len(st.MissingKeys) == 0

	if ap, err := h.Probe(); err == nil {
		st.Provider = ap.Name
		st.BaseURL = ap.BaseURL
	}
	return st
}
