package riskgate

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// cache 是判定结果缓存。
//
// 为什么必要：codex 每轮都重发整段历史，同一句用户消息会在连续请求里重复出现；
// 不缓存就会对同一句话反复调模型（这也是闸门成本的主要来源）。
// key = profile + sha256(用户文本) + sha256(注入文本)。
type cache struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[string]cacheEntry
	now func() time.Time
}

type cacheEntry struct {
	verdict Verdict
	at      time.Time
}

func newCache(ttl time.Duration) *cache {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return &cache{ttl: ttl, m: make(map[string]cacheEntry), now: time.Now}
}

func cacheKey(profile string, p Payload) string {
	h := sha256.New()
	h.Write([]byte(profile))
	h.Write([]byte{0})
	h.Write([]byte(p.UserText))
	h.Write([]byte{0})
	h.Write([]byte(p.InjectionText))
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func (c *cache) get(key string) (Verdict, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok {
		return Verdict{}, false
	}
	if c.now().Sub(e.at) > c.ttl {
		delete(c.m, key)
		return Verdict{}, false
	}
	return e.verdict, true
}

func (c *cache) put(key string, v Verdict) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = cacheEntry{verdict: v, at: c.now()}
}

// Len 供测试与统计使用。
func (c *cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.m)
}
