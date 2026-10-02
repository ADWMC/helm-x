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
	Bytes       int    `json:"bytes"`
	Default     bool   `json:"default"`
	Active      bool   `json:"active"`
	Preview     string `json:"preview"`
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
			Bytes:       m.Bytes,
			Default:     m.Default,
			Active:      m.ID == cur,
			Preview:     head(body, 300),
		})
	}
	return out, nil
}

// Current 返回当前模式 ID。
func (p *PromptService) Current() (string, error) {
	return p.s.rt.Store.Get().PromptMode, nil
}

// Set 切换模式。下次请求即生效（代理每请求读取配置，INV-5）。
func (p *PromptService) Set(mode string) error {
	if !assets.IsValidPromptMode(mode) {
		return fmt.Errorf("未知的提示词模式: %q", mode)
	}
	if err := p.s.rt.Store.Update(func(s *config.Settings) { s.PromptMode = mode }); err != nil {
		return err
	}
	p.s.rt.Logger.Info("提示词模式已切换为 %s", mode)
	p.s.emit(EventConfigChg, nil)
	return nil
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
