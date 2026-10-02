package svc

import (
	"fmt"
	"sync"

	"github.com/ADWMC/helm-x/internal/assets"
	"github.com/ADWMC/helm-x/internal/logging"
	"github.com/ADWMC/helm-x/internal/proxy"
	"github.com/ADWMC/helm-x/internal/selfcheck"
)

// VerifyService 暴露自检。自检可能跑几十秒，因此做成异步 + 进度事件。
type VerifyService struct {
	s *Services

	mu      sync.Mutex
	running bool
	last    *selfcheck.Report
}

// VerifyProgress 是一条进度事件。
type VerifyProgress struct {
	Step   int    `json:"step"`
	Total  int    `json:"total"`
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Skip   bool   `json:"skip"`
}

// Run 异步执行自检，进度通过 proxy:verify 事件推送。
//
// 立即返回；结果通过事件送达，界面无需轮询（对比旧版的 /api/zxwn 轮询）。
func (v *VerifyService) Run(e2e bool) error {
	v.mu.Lock()
	if v.running {
		v.mu.Unlock()
		return fmt.Errorf("自检正在运行")
	}
	v.running = true
	v.mu.Unlock()

	go func() {
		defer func() {
			v.mu.Lock()
			v.running = false
			v.mu.Unlock()
		}()

		rep := selfcheck.Run(e2e, func(step, total int, c selfcheck.Check) {
			v.s.emit(EventVerify, VerifyProgress{
				Step: step, Total: total,
				Name: c.Name, OK: c.OK, Detail: c.Detail, Skip: c.Skip,
			})
		})

		v.mu.Lock()
		v.last = &rep
		v.mu.Unlock()

		v.s.rt.Logger.Info("自检完成：%d 项失败", rep.Failed)
		v.s.emit(EventVerify, map[string]any{"done": true, "report": rep})
	}()
	return nil
}

// Report 返回上次自检结果（文本形式，便于直接展示）。
func (v *VerifyService) Report() (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.last == nil {
		return "", fmt.Errorf("尚未执行过自检")
	}
	return v.last.Text(), nil
}

// Running 报告是否正在自检。
func (v *VerifyService) Running() (bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.running, nil
}

// LogService 暴露日志与请求记录。
type LogService struct {
	s *Services
}

// LogLine 是前端可见的日志行。
type LogLine struct {
	TS    string `json:"ts"`
	Level string `json:"level"`
	Text  string `json:"text"`
}

// Tail 返回最近 n 条日志。
func (l *LogService) Tail(n int) ([]LogLine, error) {
	if n <= 0 {
		n = 300
	}
	lines := l.s.rt.Logger.Tail(n)
	out := make([]LogLine, 0, len(lines))
	for _, ln := range lines {
		out = append(out, LogLine{
			TS:    ln.TS.Format("15:04:05"),
			Level: string(ln.Level),
			Text:  ln.Text,
		})
	}
	return out, nil
}

// Path 返回日志文件路径。
func (l *LogService) Path() (string, error) { return l.s.rt.Logger.Path(), nil }

// RecentRequests 返回最近的请求记录。
func (l *LogService) RecentRequests(limit int) ([]proxy.RequestRecord, error) {
	if limit <= 0 {
		limit = 200
	}
	l.s.mu.Lock()
	defer l.s.mu.Unlock()
	if len(l.s.recent) <= limit {
		out := make([]proxy.RequestRecord, len(l.s.recent))
		copy(out, l.s.recent)
		return out, nil
	}
	out := make([]proxy.RequestRecord, limit)
	copy(out, l.s.recent[len(l.s.recent)-limit:])
	return out, nil
}

// ClearRequests 清空界面侧的请求记录（不影响日志文件）。
func (l *LogService) ClearRequests() error {
	l.s.mu.Lock()
	l.s.recent = nil
	l.s.mu.Unlock()
	return nil
}

// QAService 暴露常见问题。
type QAService struct {
	s *Services
}

// QAView 是 QA 数据及其来源。
type QAView struct {
	Version   int             `json:"version"`
	UpdatedAt string          `json:"updatedAt"`
	Source    string          `json:"source"`
	Items     []assets.QAItem `json:"items"`
}

// List 返回内嵌的 QA 数据。
//
// 云更新（GitHub → 缓存 → 内置）尚未实现，当前只返回内置。
// 来源字段会如实标明，不假装已联网。
func (q *QAService) List() (QAView, error) {
	d := assets.QA()
	return QAView{
		Version:   d.Version,
		UpdatedAt: d.UpdatedAt,
		Source:    "内置",
		Items:     d.Items,
	}, nil
}

var _ = logging.LevelInfo
