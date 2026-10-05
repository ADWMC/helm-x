// inject.go 是注入频率调度：按会话采样，被覆盖会话全程跟随。
//
// 语义（默认假设，实现以此为准）：
//   - 会话键取 Session-Id / Thread-Id 头；codex 不发这些头（2026-10-05 实测），
//     退化为请求里首条用户消息的指纹（RequestView.SessionFingerprint）；
//   - N = 会话采样率：第 1、N+1、2N+1… 个会话命中（"每 N 个会话注入 1 个"）；
//     N<=1 = 每个会话都命中（默认，兼容旧行为）；
//   - 命中会话注入成功后**会话内跟随**——同会话后续请求持续携带。原因：
//     每发一个请求模型都是无状态重建上下文，只注首轮则续轮全裸（实测
//     裸请求 3/12 拒绝，全覆盖 0/12）；
//   - **只有注入成功才进入跟随态**：注入尝试失败（结构不可改写、插入
//     出错等）不占名额，下次请求立即重试；
//   - 只有"可改写且有指令"的请求才进入调度 —— 透传/解析失败/无指令
//     的请求不占名额。
//
// 计数器有界：超过 4096 个会话时清理一小时内没活动的条目。
package proxy

import (
	"sync"
	"time"
)

// injectSched 按会话维护注入节奏，可并发使用。
type injectSched struct {
	mu sync.Mutex
	m  map[string]*injectCount
	// sessions 是迄今见过的会话数（含未命中的），决定采样位。
	sessions int
}

type injectCount struct {
	// covered 是本会话按采样命中（第 1、N+1、… 个会话）。
	covered bool
	// stuck 是跟随态：本会话已注入成功，后续请求持续携带。
	stuck bool
	last  time.Time
}

// shouldAttempt 返回本次是否应尝试注入。
//
// 命中会话（或已跟随）返回 true；返回 true 时调用方必须在**注入成功后**
// 调 markInjected 进入跟随态；失败则不调 —— 下次请求会再次尝试，命中
// 不作废。
func (s *injectSched) shouldAttempt(session string, every int, now time.Time) bool {
	if every <= 1 {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = make(map[string]*injectCount)
	}
	if len(s.m) > 4096 {
		for k, v := range s.m {
			if now.Sub(v.last) > time.Hour {
				delete(s.m, k)
			}
		}
	}
	c := s.m[session]
	if c == nil {
		c = &injectCount{}
		s.sessions++
		c.covered = s.sessions%every == 1
		s.m[session] = c
	}
	c.last = now
	return c.stuck || c.covered
}

// markInjected 注入成功：进入会话跟随态。
func (s *injectSched) markInjected(session string, every int, now time.Time) {
	if every <= 1 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		return
	}
	if c := s.m[session]; c != nil {
		c.stuck = true
	}
}

// shouldInject 判断本次请求是否应尝试注入并推进跳过位。
// 只在"可改写且有指令"时调用（handler.go 的 Inject 步骤）。
func (e *Engine) shouldInject(session string) bool {
	every := 1
	if e.opts.Config != nil {
		every = e.opts.Config.InjectEvery()
	}
	return e.inject.shouldAttempt(session, every, time.Now())
}

// markInjected 通知调度：本次注入成功，开启下一轮计数。
func (e *Engine) markInjected(session string) {
	every := 1
	if e.opts.Config != nil {
		every = e.opts.Config.InjectEvery()
	}
	e.inject.markInjected(session, every, time.Now())
}
