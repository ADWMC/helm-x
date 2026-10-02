// Package logging 提供线程安全日志与结构化事件。
//
// 职责：把日志写文件、同时广播给事件订阅者（GUI）。
// 不做业务判断，不解析日志内容。
//
// 日志位置与旧版一致：
//
//	~/.codex/helmx.log         代理日志
//	~/.codex/helmx-cyber.log   cyber 事件日志
package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Level 是日志级别。
type Level string

const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Line 是一条日志。
type Line struct {
	TS    time.Time `json:"ts"`
	Level Level     `json:"level"`
	Text  string    `json:"text"`
}

// Logger 是并发安全的日志器。
type Logger struct {
	mu      sync.Mutex
	file    *os.File
	path    string
	ring    []Line
	ringMax int
	subs    map[int]chan Line
	nextSub int
}

// New 创建日志器并打开文件。dir 为日志目录（通常是 codex home）。
// 打开失败时仍返回可用对象（只写内存 + 订阅者）。
func New(dir string) *Logger {
	l := &Logger{ringMax: 2000, subs: map[int]chan Line{}}
	if dir == "" {
		return l
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return l
	}
	p := filepath.Join(dir, "helmx.log")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return l
	}
	l.file = f
	l.path = p
	return l
}

// Path 返回日志文件路径。
func (l *Logger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Log 写入一条日志。
func (l *Logger) Log(level Level, format string, args ...any) {
	if l == nil {
		return
	}
	text := fmt.Sprintf(format, args...)
	if len(args) == 0 {
		text = format
	}
	line := Line{TS: time.Now(), Level: level, Text: text}

	l.mu.Lock()
	if l.file != nil {
		ts := line.TS.Format("2006-01-02 15:04:05.000")
		_, _ = fmt.Fprintf(l.file, "[%s] [%s] %s\n", ts, level, text)
	}
	l.ring = append(l.ring, line)
	if len(l.ring) > l.ringMax {
		l.ring = l.ring[len(l.ring)-l.ringMax:]
	}
	subs := make([]chan Line, 0, len(l.subs))
	for _, ch := range l.subs {
		subs = append(subs, ch)
	}
	l.mu.Unlock()

	// 非阻塞广播：订阅者慢就丢，不拖住代理
	for _, ch := range subs {
		select {
		case ch <- line:
		default:
		}
	}
}

// Info / Warn / Error 是便捷方法。
func (l *Logger) Info(format string, args ...any)  { l.Log(LevelInfo, format, args...) }
func (l *Logger) Warn(format string, args ...any)  { l.Log(LevelWarn, format, args...) }
func (l *Logger) Error(format string, args ...any) { l.Log(LevelError, format, args...) }

// Tail 返回最近 n 条日志。
func (l *Logger) Tail(n int) []Line {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if n <= 0 || n > len(l.ring) {
		n = len(l.ring)
	}
	out := make([]Line, n)
	copy(out, l.ring[len(l.ring)-n:])
	return out
}

// Subscribe 订阅日志流，返回接收通道与取消函数。
func (l *Logger) Subscribe() (<-chan Line, func()) {
	if l == nil {
		ch := make(chan Line)
		close(ch)
		return ch, func() {}
	}
	l.mu.Lock()
	id := l.nextSub
	l.nextSub++
	ch := make(chan Line, 256)
	l.subs[id] = ch
	l.mu.Unlock()

	return ch, func() {
		l.mu.Lock()
		delete(l.subs, id)
		l.mu.Unlock()
		close(ch)
	}
}

// Close 关闭日志文件。
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		err := l.file.Close()
		l.file = nil
		return err
	}
	return nil
}

// Cyber 记录一条 cyber 事件到独立文件（与旧版 helmx-cyber.log 一致）。
func (l *Logger) Cyber(dir string, entry map[string]any) {
	if dir == "" {
		return
	}
	p := filepath.Join(dir, "helmx-cyber.log")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()

	ts := time.Now().Format("2006-01-02 15:04:05")
	_, _ = fmt.Fprintf(f, "[%s] ", ts)
	first := true
	for k, v := range entry {
		if !first {
			_, _ = fmt.Fprint(f, " ")
		}
		_, _ = fmt.Fprintf(f, "%s=%v", k, v)
		first = false
	}
	_, _ = fmt.Fprintln(f)
}
