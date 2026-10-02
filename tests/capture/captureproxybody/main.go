// captureproxybody 把"经旧版代理转发出去的请求体"抓下来，用于对比。
//
// 场景：旧版代理 → 上游返回 502，但直连同样的注入内容返回 200。
// 需要确认旧版实际发出去的 body 与我手工构造的有什么差异。
//
// 做法：让旧版代理把上游指向本记录器，记录器完整落盘旧版发出的 body。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:19001", "listen address")
	outDir := flag.String("out", ".", "output directory")
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatal(err)
	}

	var seq int64
	var mu sync.Mutex

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		n := atomic.AddInt64(&seq, 1)

		name := filepath.Join(*outDir, fmt.Sprintf("%03d-body.json", n))
		_ = os.WriteFile(name, body, 0o644)

		mu.Lock()
		log.Printf("#%d %s %s body=%dB", n, r.Method, r.URL.Path, len(body))
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"id":     fmt.Sprintf("resp_%d", n),
			"object": "response",
			"status": "completed",
			"model":  "capture",
			"output": []any{
				map[string]any{
					"type": "message",
					"role": "assistant",
					"content": []any{
						map[string]any{"type": "output_text", "text": "ok"},
					},
				},
			},
			"created_at": time.Now().Unix(),
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	fmt.Fprintf(os.Stderr, "captureproxybody on %s -> %s\n", *addr, *outDir)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
