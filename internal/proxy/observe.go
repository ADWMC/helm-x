// observe.go 是观测层的**接线**：把请求链路上的四个点交给 obs。
//
// 落盘编号沿用 helm-a 的读法（数字越小越靠前）：
//
//	00.request.meta.json   入站元信息（路径/UA/会话/字节数）
//	01.request.body        入站原始报文（改写前）
//	02.upstream.body       实际发往上游的报文（注入 + 改写后，与 01 对照看效果）
//	03.response.body       上游响应正文（流式只落窗口内头部，见 pumpStream）
//	04.verdict.json        本次判定与动作（与请求列表同构）
//
// 捕获是旁路：任何失败只走 Capture 的错误回调，绝不影响转发。
package proxy

import (
	"encoding/json"

	"github.com/ADWMC/helm-x/internal/obs"
)

// capPut 写一个捕获文件（未启用时是空操作）。
func (e *Engine) capPut(id, name string, data []byte) {
	if e.opts.Capture == nil || !e.opts.Capture.Enabled() || id == "" {
		return
	}
	e.opts.Capture.Put(id, name, data)
}

// capMeta 写入站元信息。
func (e *Engine) capMeta(rec *RequestRecord, ua, contentType string) {
	if e.opts.Capture == nil || !e.opts.Capture.Enabled() {
		return
	}
	m := map[string]any{
		"path":        rec.Path,
		"sessionId":   rec.SessionID,
		"inBytes":     rec.InBytes,
		"userAgent":   ua,
		"contentType": contentType,
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	e.opts.Capture.Put(rec.ID, "00.request.meta.json", b)
}

// finishObserve 在请求收尾时落判定、索引与 JSONL 记录。
func (e *Engine) finishObserve(rec RequestRecord) {
	if e.opts.Capture != nil && e.opts.Capture.Enabled() {
		if b, err := json.MarshalIndent(rec, "", "  "); err == nil {
			e.opts.Capture.Put(rec.ID, "04.verdict.json", b)
		}
		e.opts.Capture.Index(obs.IndexEntry{
			TS:       rec.TS.Format("2006-01-02T15:04:05.000Z07:00"),
			ReqID:    rec.ID,
			Path:     rec.Path,
			Class:    string(rec.Class),
			InBytes:  rec.InBytes,
			OutBytes: rec.OutBytes,
			Note:     rec.Note,
		})
	}
	if e.opts.Recorder != nil {
		if b, err := json.Marshal(rec); err == nil {
			e.opts.Recorder.Append(b)
		}
	}
}
