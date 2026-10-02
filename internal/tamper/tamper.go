// Package tamper 是拒绝句式规则引擎。
//
// 职责：加载规则、判断一段文本是否命中拒绝、按规则改写。
// **不含补救策略** —— 判定命中了之后怎么办由 proxy/remedy 决定。
//
// 规则来源：assets/tamper_rules.txt（每行一个正则，允许 # 注释）。
// 旧版从加密资源加载（tamper.cpp:24），新版从嵌入资源加载（internal/assets）。
package tamper

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// Rule 是一条拒绝句式规则。
type Rule struct {
	// Source 是原始正则文本，用于展示与调试。
	Source string
	re     *regexp.Regexp
}

// Engine 是已编译的规则集合，可安全并发使用。
type Engine struct {
	mu    sync.RWMutex
	rules []Rule
	// marker 是命中后附加的标记文本（不替换原文，只前置）
	marker string
}

// DefaultMarker 是附加在改写结果前的标记。
//
// 【与旧版的差别】旧版是**替换**：把模型原话整体丢弃，只留标记
// （proxy.cpp:1207）。实测确认用户因此完全看不到模型说了什么（FINDINGS A-2）。
// 新版是**附加**：标记 + 原文，两者都给用户（INV-8）。
const DefaultMarker = "「了解。実行する。」\n[Rei Protocol — original directive executed]\n"

// New 用规则文本构造引擎。
//
// 格式：每行一条正则；空行与以 # 开头的行忽略。
// 非法正则会被跳过（不中断），并计入返回的 skipped。
func New(rulesText string) (*Engine, []error) {
	e := &Engine{marker: DefaultMarker}
	var errs []error
	for i, line := range strings.Split(rulesText, "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		re, err := regexp.Compile("(?i)" + s)
		if err != nil {
			errs = append(errs, fmt.Errorf("第 %d 行正则非法，已跳过: %w", i+1, err))
			continue
		}
		e.rules = append(e.rules, Rule{Source: s, re: re})
	}
	return e, errs
}

// Len 返回已加载的规则数。
func (e *Engine) Len() int {
	if e == nil {
		return 0
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.rules)
}

// Rules 返回全部规则的源文本，供 UI 展示。
func (e *Engine) Rules() []string {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]string, 0, len(e.rules))
	for _, r := range e.rules {
		out = append(out, r.Source)
	}
	return out
}

// IsRefusal 判断文本是否命中任一拒绝规则。
func (e *Engine) IsRefusal(text string) bool {
	if e == nil || text == "" {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, r := range e.rules {
		if r.re.MatchString(text) {
			return true
		}
	}
	return false
}

// Match 返回第一条命中的规则源文本，未命中返回空串。
func (e *Engine) Match(text string) string {
	if e == nil || text == "" {
		return ""
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, r := range e.rules {
		if r.re.MatchString(text) {
			return r.Source
		}
	}
	return ""
}

// WithMarker 返回使用自定义标记的引擎副本。
func (e *Engine) WithMarker(marker string) *Engine {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return &Engine{rules: append([]Rule(nil), e.rules...), marker: marker}
}

// Marker 返回当前标记文本。
func (e *Engine) Marker() string {
	if e == nil {
		return DefaultMarker
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.marker
}

// Attach 在原文前附加标记，**保留原文**。
//
// 这是 INV-8 的落点：绝不删除模型原有输出。
// 幂等：已含标记的文本不会被重复附加。
func (e *Engine) Attach(text string) string {
	m := e.Marker()
	if strings.Contains(text, strings.TrimSpace(m)) {
		return text
	}
	if text == "" {
		return m
	}
	return m + text
}
