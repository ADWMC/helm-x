package proxy

import (
	"net/http"
)

func (e *Engine) writeResponse(w http.ResponseWriter, st ResponseState) {
	ct := "application/json"
	if st.SSE {
		ct = "text/event-stream; charset=utf-8"
	}
	status := st.Status
	if status == 0 {
		status = http.StatusBadGateway
	}
	body := st.rawBody
	if len(body) == 0 {
		body = []byte(`{"error":{"message":"上游返回空响应","type":"upstream_error","code":"empty_response"}}`)
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// dryRun 计算并报告将要发送的内容，但不发上游。
func (e *Engine) dryRun(w http.ResponseWriter, r *http.Request, body []byte, rec *RequestRecord) {
	view, err := ParseRequest(r.URL.Path, body)
	if err != nil {
		e.logf("warn", "dry-run: 请求体无法解析: %v", err)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"dryRun":true,"parsed":false}`))
		rec.Class = ClassMalformed
		return
	}
	report := map[string]any{
		"dryRun":     true,
		"parsed":     true,
		"model":      view.Model,
		"stream":     view.Stream,
		"inputCount": view.InputCount(),
		"inBytes":    len(body),
	}
	instruction := e.instruction()
	report["instructionBytes"] = len(instruction)
	if instruction != "" {
		if injected, ok, ierr := view.InjectSystem(instruction); ierr == nil {
			report["injected"] = ok
			report["outBytes"] = len(injected)
		}
	}
	if msg, ok := view.LastUserMessage(); ok {
		report["lastUserMessage"] = truncateRunes(msg, 200)
	}
	rec.Class = ClassHealthy
	rec.Action = ActionNone
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, report)
}
