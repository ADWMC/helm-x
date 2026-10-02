package svc

import (
	"fmt"

	"github.com/ADWMC/helm-x/internal/codexcfg"
	"github.com/ADWMC/helm-x/internal/config"
)

// ConfigService 暴露 codex 配置的读状态与危险操作。
type ConfigService struct {
	s *Services
}

// State 返回 codex 配置状态。
func (c *ConfigService) State() (CodexState, error) {
	_, cs, _ := c.s.statusSnapshot()
	return cs, nil
}

// ConfigDiff 描述一次将要发生的修改，供危险操作确认弹窗展示。
//
// 对应 DESIGN.md §3.1：必须告诉用户"改哪个文件、改什么、怎么撤销"。
type ConfigDiff struct {
	TargetFile string `json:"targetFile"`
	Key        string `json:"key"`
	OldValue   string `json:"oldValue"`
	NewValue   string `json:"newValue"`
	BackupPath string `json:"backupPath"`
	UndoHint   string `json:"undoHint"`
}

// Preview 计算 apply 会带来什么变化，但不写入。
func (c *ConfigService) Preview() ([]ConfigDiff, error) {
	h, ok := c.s.rt.Home()
	if !ok {
		return nil, fmt.Errorf("未找到 codex 配置目录")
	}
	probe, err := h.Probe()
	if err != nil {
		return nil, err
	}
	out := []ConfigDiff{{
		TargetFile: h.ConfigPath(),
		Key:        fmt.Sprintf("[model_providers.%s].base_url", probe.Name),
		OldValue:   probe.BaseURL,
		NewValue:   c.s.rt.Store.ProxyBaseURL(),
		BackupPath: h.ProxyBakPath(),
		UndoHint:   "点击「还原配置」或执行 helmx proxy --restore",
	}}
	for _, kv := range codexcfg.ContextDefaultKeys {
		out = append(out, ConfigDiff{
			TargetFile: h.ConfigPath(),
			Key:        kv.Key,
			OldValue:   "(读取中)",
			NewValue:   kv.Val,
			BackupPath: h.BakPath(),
			UndoHint:   "点击「移除注入」或执行 helmx remove",
		})
	}
	return out, nil
}

// ApplyResult 是 apply / remove 的结果。
type ApplyResult struct {
	OK      bool     `json:"ok"`
	Changed []string `json:"changed"`
	Message string   `json:"message"`
}

// Apply 注入配置。
func (c *ConfigService) Apply() (ApplyResult, error) {
	h, ok := c.s.rt.Home()
	if !ok {
		return ApplyResult{}, fmt.Errorf("未找到 codex 配置目录")
	}
	res, err := h.Apply(codexcfg.ApplyOptions{ProxyAddr: c.s.rt.Store.ProxyBaseURL()})
	if err != nil {
		return ApplyResult{OK: false, Message: err.Error()}, err
	}

	changed := []string{}
	if res.NewBaseURL != "" {
		changed = append(changed, fmt.Sprintf("base_url → %s", res.NewBaseURL))
	}
	for _, k := range res.InjectedKey {
		changed = append(changed, "注入 "+k)
	}
	if len(changed) == 0 {
		changed = append(changed, "配置已是最新，无需改动")
	}
	if res.BackupMade {
		changed = append(changed, "已备份原文到 "+res.BackupPath)
	}

	c.s.rt.Logger.Info("配置已注入：%v", changed)
	c.s.emit(EventConfigChg, nil)
	return ApplyResult{OK: true, Changed: changed, Message: "注入完成"}, nil
}

// Remove 还原并清理注入。
func (c *ConfigService) Remove() (ApplyResult, error) {
	h, ok := c.s.rt.Home()
	if !ok {
		return ApplyResult{}, fmt.Errorf("未找到 codex 配置目录")
	}
	res, err := h.Remove(codexcfg.RemoveOptions{})
	if err != nil {
		return ApplyResult{OK: false, Message: err.Error()}, err
	}
	if res.NothingToDo {
		return ApplyResult{OK: true, Message: "没有可还原的备份"}, nil
	}

	c.s.rt.Logger.Info("配置已从备份还原")
	c.s.emit(EventConfigChg, nil)
	return ApplyResult{OK: true, Changed: []string{"config.toml 已还原"}, Message: "还原完成"}, nil
}

// RestoreProxy 只还原 base_url（保留注入）。
func (c *ConfigService) RestoreProxy() (ApplyResult, error) {
	h, ok := c.s.rt.Home()
	if !ok {
		return ApplyResult{}, fmt.Errorf("未找到 codex 配置目录")
	}
	done, err := h.RestoreProxy()
	if err != nil {
		return ApplyResult{OK: false, Message: err.Error()}, err
	}
	msg := "上游地址已还原"
	if !done {
		msg = "配置本就未指向本地代理"
	}
	c.s.emit(EventConfigChg, nil)
	return ApplyResult{OK: true, Message: msg}, nil
}

// OpenConfigDir 返回配置文件所在目录，供界面显示。
func (c *ConfigService) OpenConfigDir() (string, error) {
	h, ok := c.s.rt.Home()
	if !ok {
		return "", fmt.Errorf("未找到 codex 配置目录")
	}
	return h.Dir, nil
}

// Settings 返回可编辑的运行设置。
func (c *ConfigService) Settings() (config.Settings, error) {
	return c.s.rt.Store.Get(), nil
}

// SaveSettings 保存运行设置（立即生效，INV-5）。
func (c *ConfigService) SaveSettings(v config.Settings) error {
	if err := c.s.rt.Store.Save(v); err != nil {
		return err
	}
	c.s.emit(EventConfigChg, nil)
	return nil
}
