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
	"strings"
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

// DefaultMaxBytes 是单个日志文件的上限（超过后轮转为 .1）。
const DefaultMaxBytes int64 = 8 << 20

// Logger 是并发安全的日志器。
type Logger struct {
	mu       sync.Mutex
	file     *os.File
	path     string
	written  int64
	maxBytes int64
	ring     []Line
	ringMax  int
	subs     map[int]chan Line
	nextSub  int
}

// New 创建日志器并打开文件。dir 为日志目录（通常是 codex home）。
// 打开失败时仍返回可用对象（只写内存 + 订阅者）。
func New(dir string) *Logger {
	l := &Logger{ringMax: 2000, subs: map[int]chan Line{}, maxBytes: DefaultMaxBytes}
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
	if st, err := f.Stat(); err == nil {
		l.written = st.Size() // 追加模式：已写字节数 = 当前文件大小
	}
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
		line := fmt.Sprintf("[%s] [%s] %s\n", line.TS.Format("2006-01-02 15:04:05.000"), level, text)
		// 轮转：单文件超过上限就转成 .1（保留一份历史，避免无限增长）
		if l.maxBytes > 0 && l.written+int64(len(line)) > l.maxBytes {
			l.rotateLocked()
		}
		n, _ := l.file.WriteString(line)
		l.written += int64(n)
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

// rotateLocked 轮转日志文件：helmx.log → helmx.log.1，重开新文件。
// 调用方必须持锁。轮转失败不致命（继续往原文件追加）。
func (l *Logger) rotateLocked() {
	if l.path == "" {
		return
	}
	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}
	old := l.path + ".1"
	_ = os.Remove(old) // Windows 上 rename 不能覆盖已存在文件
	if err := os.Rename(l.path, old); err != nil {
		// 改名失败（被占用等）：清空原文件继续用
		if f, ferr := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); ferr == nil {
			l.file = f
			l.written = 0
			return
		}
		return
	}
	if f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		l.file = f
		l.written = 0
	}
}

// Cyber 记录一条 cyber 事件到独立文件（与旧版 helmx-cyber.log 一致）。
// 同样带体积轮转，避免长期运行后文件无限增大。
func (l *Logger) Cyber(dir string, entry map[string]any) {
	if dir == "" {
		return
	}
	p := filepath.Join(dir, "helmx-cyber.log")

	var sb strings.Builder
	fmt.Fprintf(&sb, "[%s] ", time.Now().Format("2006-01-02 15:04:05"))
	first := true
	for k, v := range entry {
		if !first {
			sb.WriteString(" ")
		}
		fmt.Fprintf(&sb, "%s=%v", k, v)
		first = false
	}
	sb.WriteString("\n")

	l.mu.Lock()
	defer l.mu.Unlock()
	if st, err := os.Stat(p); err == nil && l.maxBytes > 0 && st.Size()+int64(sb.Len()) > l.maxBytes {
		_ = os.Remove(p + ".1")
		_ = os.Rename(p, p+".1")
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	_, _ = f.WriteString(sb.String())
	_ = f.Close()
}
