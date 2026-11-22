// capture.go 是「报文」页的服务端：捕获开关 + 磁盘上的捕获浏览。
//
// 与请求页互补：请求页看内存里的摘要（最近 500 条），报文页看**盘上的原始字节**
// （改写前 / 改写后 / 响应 / 判定），用于回答"到底发出去的是什么"。
package svc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/ADWMC/helm-x/internal/config"
	"path/filepath"
	"sort"
	"strings"
)

// CaptureService 暴露调试捕获的开关与浏览。
type CaptureService struct {
	s *Services
}

// CaptureState 是捕获的当前状态（给页头用）。
type CaptureState struct {
	Enabled   bool   `json:"enabled"`
	Redacted  bool   `json:"redacted"`
	Root      string `json:"root"`
	RecordDir string `json:"recordDir"`
	Bytes     int64  `json:"bytes"`
	Sessions  int    `json:"sessions"`
	QuotaMB   int    `json:"quotaMb"`
}

// CaptureItem 是 index.jsonl 的一行（给列表用）。
type CaptureItem struct {
	TS       string `json:"ts"`
	ReqID    string `json:"reqId"`
	Path     string `json:"path"`
	Class    string `json:"class"`
	InBytes  int    `json:"inBytes"`
	OutBytes int    `json:"outBytes"`
	Note     string `json:"note,omitempty"`
}

// CaptureFile 是一个可读的捕获文件。
type CaptureFile struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// State 返回捕获状态。
func (c *CaptureService) State() (CaptureState, error) {
	cap := c.s.rt.Capture()
	if cap == nil {
		return CaptureState{}, fmt.Errorf("捕获未初始化")
	}
	st := CaptureState{
		Enabled:  cap.Enabled(),
		Redacted: cap.Redacted(),
		Root:     cap.Root(),
		QuotaMB:  512,
	}
	if rec := c.s.rt.Recorder(); rec != nil {
		st.RecordDir = c.s.rt.RecordDir()
	}
	st.Bytes, st.Sessions = scanCapture(cap.Root())
	return st, nil
}

// SetEnabled 热切换捕获开关（关闭不清已捕获文件）。
func (c *CaptureService) SetEnabled(on bool) error {
	cap := c.s.rt.Capture()
	if cap == nil {
		return fmt.Errorf("捕获未初始化")
	}
	cap.SetEnabled(on)
	if err := c.s.rt.Store.Update(func(s *config.Settings) { s.CaptureEnabled = on }); err != nil {
		return err
	}
	c.s.rt.Logger.Info("调试捕获已%s", map[bool]string{true: "开启", false: "关闭"}[on])
	c.s.emit(EventConfigChg, nil)
	return nil
}

// SetRedact 热切换脱敏。
func (c *CaptureService) SetRedact(on bool) error {
	cap := c.s.rt.Capture()
	if cap == nil {
		return fmt.Errorf("捕获未初始化")
	}
	cap.SetRedact(on)
	if err := c.s.rt.Store.Update(func(s *config.Settings) { s.CaptureRedact = on }); err != nil {
		return err
	}
	c.s.emit(EventConfigChg, nil)
	return nil
}

// List 读 index.jsonl 的最近 limit 条（倒序：最新在前）。
func (c *CaptureService) List(limit int) ([]CaptureItem, error) {
	cap := c.s.rt.Capture()
	if cap == nil {
		return nil, fmt.Errorf("捕获未初始化")
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	f, err := os.Open(filepath.Join(cap.Root(), "index.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // 还没捕获过：空列表不是错误
		}
		return nil, err
	}
	defer f.Close()

	var items []CaptureItem
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var it CaptureItem
		if err := json.Unmarshal([]byte(line), &it); err != nil {
			continue // 坏行跳过，不因一行废掉整页
		}
		items = append(items, it)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	// 倒序 + 截断
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

// Files 列出某请求目录下的文件（按名排序，编号即时间线）。
func (c *CaptureService) Files(reqID string) ([]CaptureFile, error) {
	dir, err := c.reqDir(reqID)
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]CaptureFile, 0, len(ents))
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		info, ierr := e.Info()
		var size int64
		if ierr == nil {
			size = info.Size()
		}
		out = append(out, CaptureFile{Name: e.Name(), Size: size})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Read 读一个捕获文件的内容（上限 4MB，超大截断并标注）。
func (c *CaptureService) Read(reqID, name string) (string, error) {
	dir, err := c.reqDir(reqID)
	if err != nil {
		return "", err
	}
	if strings.ContainsAny(name, `/\`) || name == "" {
		return "", fmt.Errorf("非法文件名: %q", name)
	}
	full := filepath.Join(dir, name)
	info, err := os.Stat(full)
	if err != nil {
		return "", err
	}
	const max = 4 << 20
	f, err := os.Open(full)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, min64(info.Size(), max))
	if _, err := f.Read(buf); err != nil && info.Size() > 0 {
		return "", err
	}
	s := string(buf)
	if info.Size() > max {
		s += fmt.Sprintf("\n…（已截断：原文件 %d 字节，只显示前 %d 字节）", info.Size(), max)
	}
	return s, nil
}

// OpenDir 在资源管理器里打开捕获根目录。
func (c *CaptureService) OpenDir() error {
	cap := c.s.rt.Capture()
	if cap == nil {
		return fmt.Errorf("捕获未初始化")
	}
	if err := os.MkdirAll(cap.Root(), 0o700); err != nil {
		return err
	}
	return openPath(cap.Root())
}

// reqDir 返回某请求的捕获目录（带路径校验，不接受 .. 之类）。
func (c *CaptureService) reqDir(reqID string) (string, error) {
	cap := c.s.rt.Capture()
	if cap == nil {
		return "", fmt.Errorf("捕获未初始化")
	}
	if reqID == "" || strings.ContainsAny(reqID, `/\`) || reqID == ".." {
		return "", fmt.Errorf("非法请求 id: %q", reqID)
	}
	root := cap.Root()
	// 目录名带日期前缀：扫一遍找到包含该 reqID 的那天
	ents, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		cand := filepath.Join(root, e.Name(), reqID)
		if st, serr := os.Stat(cand); serr == nil && st.IsDir() {
			return cand, nil
		}
	}
	return "", fmt.Errorf("找不到请求 %q 的捕获目录", reqID)
}

func openPath(p string) error {
	cmd := exec.Command("explorer", p)
	return cmd.Start()
}

func scanCapture(root string) (bytes int64, sessions int) {
	ents, err := os.ReadDir(root)
	if err != nil {
		return 0, 0
	}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		dayDir := filepath.Join(root, e.Name())
		subs, serr := os.ReadDir(dayDir)
		if serr != nil {
			continue
		}
		sessions += len(subs)
		_ = filepath.Walk(dayDir, func(_ string, info os.FileInfo, werr error) error {
			if werr == nil && info != nil && !info.IsDir() {
				bytes += info.Size()
			}
			return nil
		})
	}
	return bytes, sessions
}

func min64(a int64, b int) int {
	if a < int64(b) {
		return int(a)
	}
	return b
}
