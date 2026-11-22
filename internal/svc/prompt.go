package svc

import (
	"fmt"

	"github.com/ADWMC/helm-x/internal/assets"
	"github.com/ADWMC/helm-x/internal/config"
)

// PromptService 暴露提示词模式。
type PromptService struct {
	s *Services
}

// PromptModeView 是前端可见的模式信息。
type PromptModeView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Models      string `json:"models"`
	Bytes       int    `json:"bytes"`
	Default     bool   `json:"default"`
	Active      bool   `json:"active"`
	Preview     string `json:"preview"`
	// Deploy 是该模式实测过的部署语义；"replace-base" 表示必须整段替换 instructions。
	Deploy string `json:"deploy,omitempty"`
}

// Modes 列出全部模式，标记当前激活项并附一小段预览。
func (p *PromptService) Modes() ([]PromptModeView, error) {
	cur := p.s.rt.Store.Get().PromptMode
	modes := assets.PromptModes()
	out := make([]PromptModeView, 0, len(modes))
	for _, m := range modes {
		body := assets.Prompt(m.ID)
		out = append(out, PromptModeView{
			ID:          m.ID,
			Name:        m.Name,
			Description: m.Description,
			Models:      m.Models,
			Bytes:       m.Bytes,
			Default:     m.Default,
			Active:      m.ID == cur,
			Preview:     head(body, 300),
			Deploy:      m.Deploy,
		})
	}
	return out, nil
}

// Current 返回当前模式 ID。
func (p *PromptService) Current() (string, error) {
	return p.s.rt.Store.Get().PromptMode, nil
}

// Set 切换模式。下次请求即生效（代理每请求读取配置，INV-5）。
//
// 部署语义守卫：某些模式（当前只有 ctf-ac 主臂）的实测结论以"整段替换
// instructions"为前提。选它但部署语义是 insert 时，装上去的是**另一种配置**，
// 结论不成立却看不出来 —— 所以这里直接拒绝，并给出可执行的下一步。
func (p *PromptService) Set(mode string) error {
	// 历史 ID 先归一化再落盘：配置里存的永远是现行 ID
	mode = assets.CanonicalPromptMode(mode)
	if !assets.IsValidPromptMode(mode) {
		return fmt.Errorf("未知的提示词模式: %q", mode)
	}
	if need := modeDeploy(mode); need == "replace-base" {
		cur := p.s.rt.Store.Get().Deploy
		if cur != "replace-base" {
			return fmt.Errorf("模式 %q 要求整段替换（replace-base），当前部署语义是 %q；先把「部署语义」切到 replace-base 再应用", mode, orInsert(cur))
		}
	}
	if err := p.s.rt.Store.Update(func(s *config.Settings) { s.PromptMode = mode }); err != nil {
		return err
	}
	p.s.rt.Logger.Info("提示词模式已切换为 %s", mode)
	p.s.emit(EventConfigChg, nil)
	return nil
}

// GetDeploy 返回当前部署语义（空 = insert）。
func (p *PromptService) GetDeploy() (string, error) {
	return orInsert(p.s.rt.Store.Get().Deploy), nil
}

// SetDeploy 设置部署语义：insert / append / replace-base。
func (p *PromptService) SetDeploy(mode string) error {
	switch mode {
	case "insert", "append", "replace-base":
	default:
		return fmt.Errorf("未知的部署语义: %q（可选 insert / append / replace-base）", mode)
	}
	if err := p.s.rt.Store.Update(func(s *config.Settings) { s.Deploy = mode }); err != nil {
		return err
	}
	p.s.rt.Logger.Info("部署语义已切换为 %s", mode)
	p.s.emit(EventConfigChg, nil)
	return nil
}

// ── 请求层改写（2026-11-22 迁入）──

// CodebookView 是代号表配置。
type CodebookView struct {
	Enabled bool `json:"enabled"`
	Slots   int  `json:"slots"`
}

// GetCodebook 返回代号表配置。
func (p *PromptService) GetCodebook() (CodebookView, error) {
	s := p.s.rt.Store.Get()
	return CodebookView{Enabled: s.CodebookEnabled, Slots: s.CodebookSlots}, nil
}

// SetCodebook 保存代号表配置（slots 钳到 1–8）。
func (p *PromptService) SetCodebook(enabled bool, slots int) error {
	if slots < 1 {
		slots = 1
	}
	if slots > 8 {
		slots = 8
	}
	if err := p.s.rt.Store.Update(func(s *config.Settings) {
		s.CodebookEnabled = enabled
		s.CodebookSlots = slots
	}); err != nil {
		return err
	}
	p.s.rt.Logger.Info("代号表：enabled=%v slots=%d", enabled, slots)
	p.s.emit(EventConfigChg, nil)
	return nil
}

// GetEncode 返回全文编码开关。
func (p *PromptService) GetEncode() (bool, error) {
	return p.s.rt.Store.Get().EncodeEnabled, nil
}

// SetEncode 保存全文编码开关（与代号表互斥；开启时 encode 优先）。
func (p *PromptService) SetEncode(enabled bool) error {
	if err := p.s.rt.Store.Update(func(s *config.Settings) { s.EncodeEnabled = enabled }); err != nil {
		return err
	}
	p.s.rt.Logger.Info("全文编码：enabled=%v", enabled)
	p.s.emit(EventConfigChg, nil)
	return nil
}

// ModeDeploy 返回某模式的实测部署语义（空 = insert）。
func modeDeploy(mode string) string {
	for _, m := range assets.PromptModes() {
		if m.ID == mode {
			return m.Deploy
		}
	}
	return ""
}

func orInsert(s string) string {
	if s == "" {
		return "insert"
	}
	return s
}

// Preview 返回指定模式的完整正文。
func (p *PromptService) Preview(mode string) (string, error) {
	if !assets.IsValidPromptMode(mode) {
		return "", fmt.Errorf("未知的提示词模式: %q", mode)
	}
	return assets.Prompt(mode), nil
}

// ── 关于已移除的 RulesService ──
//
// 曾有一个 RulesService，向界面暴露 TAMPER 的拒绝句式正则列表。
// 已删除，理由：
//
//  1. 它是**只读**的 —— 界面唯一交互是搜索框，不能编辑、不能保存、不能开关。
//     改规则只能改 assets/data/tamper_rules.txt 再重新构建。
//  2. 它把「实现细节」当成「用户可操作项」摆在导航里。28 条正则
//     对使用者没有决策价值：真正需要知道的只是"这个请求为什么被判定为拒绝"，
//     而那件事应该由「请求」页的判定结果回答，不需要用户自己读正则。
//  3. TAMPER 引擎本身仍在正常工作，只是不再对外暴露规则清单。
//     内部仍可通过 tamper.Engine.Rules() 读取（自检会用到规则条数）。

func head(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
