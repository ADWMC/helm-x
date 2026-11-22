// Package obs 是观测层：调试报文捕获 + 请求记录落盘（从 helm-a 迁入，2026-11-22）。
//
// 两条独立通道：
//
//	Capture   每个请求的原始报文（默认关；开启后落盘，含凭据风险，见 Redact）
//	Recorder  每个请求一行的结构化记录（JSONL，按日轮转，保留 7 天）
//
// 本包只做落盘，不认识 proxy 的类型 —— 调用方传 []byte/string，避免包循环。
package obs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ── 报文捕获 ────────────────────────────────────────────────────────────────

// 默认配额（与 helm-a 一致）：总量 512MB，超出删最旧的日期目录。
const DefaultQuotaBytes int64 = 512 << 20

// Capture 把每个请求的报文写到 <root>/<YYYY-MM-DD>/<reqID>/<name>。
type Capture struct {
	root  string
	quota int64

	enabled atomic.Bool
	redact  atomic.Bool

	mu    sync.Mutex
	onErr func(error)
}

// NewCapture 构造捕获器。enabled=false 时所有写入是空操作（热切换）。
func NewCapture(root string, enabled bool) *Capture {
	c := &Capture{root: root, quota: DefaultQuotaBytes}
	c.enabled.Store(enabled)
	c.redact.Store(true) // 默认脱敏：含凭据的头文件不该明文躺盘
	return c
}

// Root 返回捕获根目录。
func (c *Capture) Root() string { return c.root }

// Enabled 报告是否开启。
func (c *Capture) Enabled() bool { return c != nil && c.enabled.Load() }

// SetEnabled 热切换捕获开关（关闭不清已捕获文件）。
func (c *Capture) SetEnabled(v bool) {
	if c != nil {
		c.enabled.Store(v)
	}
}

// Redacted 报告是否对报文里的凭据做脱敏。
func (c *Capture) Redacted() bool { return c != nil && c.redact.Load() }

// SetRedact 热切换脱敏。
func (c *Capture) SetRedact(v bool) {
	if c != nil {
		c.redact.Store(v)
	}
}

// SetErrorHandler 接收落盘错误（捕获失败绝不能影响转发）。
func (c *Capture) SetErrorHandler(fn func(error)) {
	if c != nil {
		c.mu.Lock()
		c.onErr = fn
		c.mu.Unlock()
	}
}

func (c *Capture) fail(err error) {
	c.mu.Lock()
	fn := c.onErr
	c.mu.Unlock()
	if fn != nil {
		fn(err)
	}
}

// Put 写一个文件到某请求的目录下。name 如 "01.request.body"。
// 失败只回调错误，不返回 error —— 捕获是旁路，不许影响转发（INV-3 同源）。
func (c *Capture) Put(reqID, name string, data []byte) {
	if !c.Enabled() || reqID == "" || name == "" {
		return
	}
	dir := filepath.Join(c.root, time.Now().Format("2006-01-02"), sanitize(reqID))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		c.fail(err)
		return
	}
	if c.Redacted() {
		data = Redact(data)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		c.fail(err)
		return
	}
	c.enforce()
}

// indexEntry 是 index.jsonl 的一行。
type indexEntry struct {
	TS       string `json:"ts"`
	ReqID    string `json:"reqId"`
	Path     string `json:"path"`
	Class    string `json:"class,omitempty"`
	InBytes  int    `json:"inBytes"`
	OutBytes int    `json:"outBytes"`
	Note     string `json:"note,omitempty"`
}

// Index 追加一行索引（<root>/index.jsonl）。失败只回调错误。
func (c *Capture) Index(e IndexEntry) {
	if !c.Enabled() {
		return
	}
	if err := os.MkdirAll(c.root, 0o700); err != nil {
		c.fail(err)
		return
	}
	line, err := json.Marshal(indexEntry(e))
	if err != nil {
		c.fail(err)
		return
	}
	f, err := os.OpenFile(filepath.Join(c.root, "index.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		c.fail(err)
		return
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		c.fail(err)
	}
}

// IndexEntry 是对外暴露的索引行（避免调用方依赖内部结构）。
type IndexEntry struct {
	TS       string
	ReqID    string
	Path     string
	Class    string
	InBytes  int
	OutBytes int
	Note     string
}

// enforce 检查总量配额，超出则删最旧的日期目录。
func (c *Capture) enforce() {
	total, days := c.scan()
	if total <= c.quota || len(days) == 0 {
		return
	}
	// days 已按名字升序；从最旧的开始删，直到回到配额内
	for _, d := range days {
		if total <= c.quota {
			return
		}
		sz := dirSize(filepath.Join(c.root, d))
		if err := os.RemoveAll(filepath.Join(c.root, d)); err != nil {
			c.fail(err)
			return
		}
		total -= sz
	}
}

// scan 返回总量与按日期升序的日期目录名。
func (c *Capture) scan() (int64, []string) {
	ents, err := os.ReadDir(c.root)
	if err != nil {
		return 0, nil
	}
	var total int64
	var days []string
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if _, err := time.Parse("2006-01-02", name); err != nil {
			continue
		}
		days = append(days, name)
		total += dirSize(filepath.Join(c.root, name))
	}
	sort.Strings(days)
	return total, days
}

func dirSize(p string) int64 {
	var n int64
	_ = filepath.Walk(p, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			n += info.Size()
		}
		return nil
	})
	return n
}

// ── 凭据脱敏 ────────────────────────────────────────────────────────────────

// redactPatterns 覆盖常见凭据头（报文里有就换掉，保留头名便于排障）。
var redactPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)("?(?:authorization|api[-_]?key|x-api-key|openai-api-key)"?\s*:\s*")([^"]{4,})(")`),
	regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9._\-]{8,}`),
	regexp.MustCompile(`(?i)("?(?:cookie|set-cookie|chatgpt-account-id)"?\s*:\s*")([^"]{4,})(")`),
}

// Redact 把报文里的凭据替换成 ****（保留前后各 2 字符便于认出是哪个 key）。
func Redact(data []byte) []byte {
	s := string(data)
	for _, re := range redactPatterns {
		s = re.ReplaceAllStringFunc(s, func(m string) string {
			sub := re.FindStringSubmatch(m)
			if len(sub) == 4 {
				return sub[1] + mask(sub[2]) + sub[3]
			}
			if len(sub) == 3 {
				return sub[1] + mask(sub[2])
			}
			return m
		})
	}
	return []byte(s)
}

func mask(s string) string {
	if len(s) <= 6 {
		return "****"
	}
	return s[:2] + "****" + s[len(s)-2:]
}

func sanitize(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		}
		return '_'
	}, s)
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

// ── 请求记录落盘（JSONL，按日轮转，保留 7 天）──────────────────────────────

// Recorder 把每条请求记录追加到 <root>/requests-YYYY-MM-DD.jsonl。
type Recorder struct {
	dir   string
	keep  int
	mu    sync.Mutex
	onErr func(error)
}

// NewRecorder 构造记录器；dir 通常与 capture 根同级。
func NewRecorder(dir string, keepDays int) *Recorder {
	if keepDays <= 0 {
		keepDays = 7
	}
	r := &Recorder{dir: dir, keep: keepDays}
	r.prune()
	return r
}

// SetErrorHandler 接收落盘错误。
func (r *Recorder) SetErrorHandler(fn func(error)) {
	r.mu.Lock()
	r.onErr = fn
	r.mu.Unlock()
}

// Append 追加一条 JSON 记录（调用方给序列化好的字节）。
func (r *Recorder) Append(line []byte) {
	if r == nil || len(line) == 0 {
		return
	}
	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		r.report(err)
		return
	}
	p := filepath.Join(r.dir, "requests-"+time.Now().Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		r.report(err)
		return
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		r.report(err)
	}
}

// Read 读取某天的记录（day 形如 2026-11-22）；空 day = 今天。
func (r *Recorder) Read(day string) ([]byte, error) {
	if r == nil {
		return nil, fmt.Errorf("recorder 未启用")
	}
	if day == "" {
		day = time.Now().Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", day); err != nil {
		return nil, fmt.Errorf("日期格式应为 YYYY-MM-DD: %q", day)
	}
	return os.ReadFile(filepath.Join(r.dir, "requests-"+day+".jsonl"))
}

// Days 返回现存日期（降序）。
func (r *Recorder) Days() []string {
	if r == nil {
		return nil
	}
	ents, err := os.ReadDir(r.dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		name := e.Name()
		if !strings.HasPrefix(name, "requests-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		out = append(out, strings.TrimSuffix(strings.TrimPrefix(name, "requests-"), ".jsonl"))
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out
}

func (r *Recorder) prune() {
	days := r.Days()
	for i, d := range days {
		if i < r.keep {
			continue
		}
		_ = os.Remove(filepath.Join(r.dir, "requests-"+d+".jsonl"))
	}
}

func (r *Recorder) report(err error) {
	r.mu.Lock()
	fn := r.onErr
	r.mu.Unlock()
	if fn != nil {
		fn(err)
	}
}
