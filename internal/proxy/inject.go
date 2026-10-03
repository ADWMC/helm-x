// inject.go 是注入频率调度：按会话计数，每 N 次请求注入 1 次。
//
// 语义（默认假设，实现以此为准）：
//   - 每个会话（Session-Id / Thread-Id，无则空串桶）各自计数；
//   - 每个会话的第 1、N+1、2N+1… 次请求注入，即首次请求就注入；
//   - N<=1 表示每次都注入（默认，兼容旧行为）；
//   - 只有"可改写且有指令"的请求才计数 —— 计数入口只在 handler 的
//     Inject 步骤里调用，透传/解析失败/无指令的请求不占名额。
//
// 计数器有界：超过 4096 个会话时清理一小时内没活动的条目。
package proxy

import (
	"sync"
	"time"
)

// injectSched 按会话维护注入计数，可并发使用。
type injectSched struct {
	mu sync.Mutex
	m  map[string]*injectCount
}

type injectCount struct {
	n    int
	last time.Time
}

// isDue 把会话计数 +1，返回本次是否到达注入点。
func (s *injectSched) isDue(session string, every int, now time.Time) bool {
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
	c.n++
	c.last = now
	return (c.n-1)%every == 0
}

// shouldInject 判断本次请求是否到达注入点，并推进计数。
// 只在"可改写且有指令"时调用（handler.go 的 Inject 步骤）。
func (e *Engine) shouldInject(session string) bool {
	every := 1
	if e.opts.Config != nil {
		every = e.opts.Config.InjectEvery()
	}
	return e.inject.isDue(session, every, time.Now())
}
