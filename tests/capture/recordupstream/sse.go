package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// wantsStream 判断请求体是否要求流式响应。
func wantsStream(body []byte) bool {
	i := bytes.Index(body, []byte(`"stream"`))
	if i < 0 {
		return false
	}
	colon := bytes.IndexByte(body[i:], ':')
	if colon < 0 {
		return false
	}
	at := i + colon + 1
	for at < len(body) && (body[at] == ' ' || body[at] == '\t') {
		at++
	}
	return at+4 <= len(body) && bytes.Equal(body[at:at+4], []byte("true"))
}

// writeJSONCompleted 返回非流式的完整响应。
func writeJSONCompleted(w http.ResponseWriter, n int64) {
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{
		"id":         fmt.Sprintf("resp_capture_%d", n),
		"object":     "response",
		"created_at": time.Now().Unix(),
		"status":     "completed",
		"model":      "capture",
		"output": []any{
			map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "output_text", "text": "ok"},
				},
			},
		},
		"usage": map[string]any{
			"input_tokens":  1,
			"output_tokens": 1,
			"total_tokens":  2,
		},
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// writeSSECompleted 返回一段最小但**合法**的 Responses API SSE 序列。
//
// 序列依据实测的真实上游响应（见 FINDINGS-phase1.md）：
//
//	response.created → response.output_item.added → response.output_text.delta
//	→ response.output_item.done → response.completed
//
// 缺少任一步都可能导致 codex 报 "stream disconnected"。
func writeSSECompleted(w http.ResponseWriter, n int64) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	fl, _ := w.(http.Flusher)
	id := fmt.Sprintf("resp_capture_%d", n)
	itemID := fmt.Sprintf("msg_capture_%d", n)

	emit := func(event string, payload map[string]any) {
		b, _ := json.Marshal(payload)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		if fl != nil {
			fl.Flush()
		}
	}

	resp := map[string]any{
		"id":         id,
		"object":     "response",
		"created_at": time.Now().Unix(),
		"status":     "in_progress",
		"model":      "capture",
	}

	emit("response.created", map[string]any{"type": "response.created", "response": resp})

	emit("response.output_item.added", map[string]any{
		"type":         "response.output_item.added",
		"output_index": 0,
		"item": map[string]any{
			"id":      itemID,
			"type":    "message",
			"role":    "assistant",
			"status":  "in_progress",
			"content": []any{},
		},
	})

	emit("response.output_text.delta", map[string]any{
		"type":          "response.output_text.delta",
		"item_id":       itemID,
		"output_index":  0,
		"content_index": 0,
		"delta":         "ok",
	})

	emit("response.output_text.done", map[string]any{
		"type":          "response.output_text.done",
		"item_id":       itemID,
		"output_index":  0,
		"content_index": 0,
		"text":          "ok",
	})

	emit("response.output_item.done", map[string]any{
		"type":         "response.output_item.done",
		"output_index": 0,
		"item": map[string]any{
			"id":     itemID,
			"type":   "message",
			"role":   "assistant",
			"status": "completed",
			"content": []any{
				map[string]any{"type": "output_text", "text": "ok"},
			},
		},
	})

	done := map[string]any{
		"id":         id,
		"object":     "response",
		"created_at": time.Now().Unix(),
		"status":     "completed",
		"model":      "capture",
		"output": []any{
			map[string]any{
				"id":     itemID,
				"type":   "message",
				"role":   "assistant",
				"status": "completed",
				"content": []any{
					map[string]any{"type": "output_text", "text": "ok"},
				},
			},
		},
		"usage": map[string]any{
			"input_tokens":  1,
			"output_tokens": 1,
			"total_tokens":  2,
		},
	}
	emit("response.completed", map[string]any{"type": "response.completed", "response": done})
}
