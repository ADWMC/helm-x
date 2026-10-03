// inject.go 是注入频率调度：每 N 次请求注入 1 次，按会话计数。
//
// 语义（默认假设，实现以此为准）：
//   - 每个会话（Session-Id / Thread-Id，无则空串桶）各自计数；
//   - 每个会话的首次请求注入，成功后每 N 次请求注入 1 次；
//   - **只有注入成功才开启下一轮计数**：注入尝试失败（结构不可改写、
//     插入出错等）不占名额，下次请求立即重试，直到成功后才重新
//     数 N 个请求再注入；
//   - N<=1 表示每次都注入（默认，兼容旧行为）；
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
}

type injectCount struct {
	// skip 是距下一次注入还需跳过的请求数；0 = 下次请求即注入点。
	skip int
	last time.Time
}

// shouldAttempt 返回本次是否应尝试注入。
//
// 跳过位 >0 时递减并返回 false；=0 返回 true。返回 true 时调用方必须
// 在**注入成功后**调 markInjected 开启下一轮；失败则不调 —— 下次请求
// 会再次尝试，槽位不作废。
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
		s.m[session] = c
	}
	c.last = now
	if c.skip > 0 {
		c.skip--
		return false
	}
	return true
}

// markInjected 注入成功：开启下一轮 N 次计数。
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
		c.skip = every - 1
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
