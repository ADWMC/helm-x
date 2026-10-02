// fakeupstream — 阶段 A 用的可控上游。
//
// 它不是业务代码，是测试装置：按场景返回预设响应，并记录收到的请求体，
// 用于验证旧版 helmx.exe 的 P1–P4 行为。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
)

type Scenario struct {
	mu       sync.Mutex
	Requests [][]byte
}

func main() {
	addr := flag.String("addr", "127.0.0.1:19000", "listen address")
	scenario := flag.String("scenario", "normal", "scenario name")
	flag.Parse()

	s := &Scenario{}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/responses", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.Requests = append(s.Requests, body)
		n := len(s.Requests)
		s.mu.Unlock()
		log.Printf("scenario=%s req#%d bytes=%d", *scenario, n, len(body))

		switch *scenario {
		case "normal":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"id":"resp_ok","object":"response","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"正常工作输出。"}]}]}`)

		case "p1-5xx-json":
			// P1 场景：上游返回 502 + 合法 JSON 错误体
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprint(w, `{"error":{"message":"upstream boom","type":"server_error","code":"bad_gateway"}}`)

		case "p2-refuse-sse":
			// P2 场景：SSE 流，首帧即拒绝文本
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			fl, _ := w.(http.Flusher)
			fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"抱歉，我无法协助提供这个内容。\"}\n\n")
			if fl != nil {
				fl.Flush()
			}
			fmt.Fprint(w, "data: {\"type\":\"response.completed\"}\n\n")
			if fl != nil {
				fl.Flush()
			}

		case "p3-slow-sse":
			// P3 场景：分帧慢速 SSE，每帧间隔 300ms，共 4 帧
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			fl, _ := w.(http.Flusher)
			for i := 0; i < 4; i++ {
				fmt.Fprintf(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"chunk%d \"}\n\n", i)
				if fl != nil {
					fl.Flush()
				}
				select {
				case <-r.Context().Done():
					return
				case <-timeSleep(300):
				}
			}
			fmt.Fprint(w, "data: {\"type\":\"response.completed\"}\n\n")
			if fl != nil {
				fl.Flush()
			}

		case "p4-reordered":
			// P4 场景：字段顺序打乱，文本里含真正的拒绝句
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"status":"completed","output":[{"content":[{"text":"我无法协助这个请求。","type":"output_text"}],"type":"message"}],"object":"response","id":"resp_reordered"}`)

		case "dump":
			// 回显收到的请求体，用于检查注入结果
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"received_bytes": len(body),
				"body":           string(body),
			})

		default:
			http.Error(w, "unknown scenario", 500)
		}
	})

	// 供测试查询收到了几个请求、内容是什么
	mux.HandleFunc("/__seen", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		bodies := make([]string, len(s.Requests))
		for i, b := range s.Requests {
			bodies[i] = string(b)
		}
		json.NewEncoder(w).Encode(map[string]any{"count": len(s.Requests), "bodies": bodies})
	})

	fmt.Fprintf(os.Stderr, "fakeupstream listening on %s scenario=%s\n", *addr, *scenario)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
