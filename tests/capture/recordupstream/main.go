// recordupstream 是一个**记录型假上游**：把收到的真实请求体存盘，并返回一个
// 最小的合法响应，让 codex 能正常结束一轮对话。
//
// 用途：抓取真实 codex 请求体作为阶段 1 的测试 fixture。
// 与 fakeupstream 的区别：fakeupstream 按场景造响应（测行为），
// 本工具忠实落盘请求（采集形态）。职责不同，故独立成程序。
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
	"strings"
	"sync"
	"sync/atomic"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:19000", "listen address")
	outDir := flag.String("out", ".", "directory to write captured request bodies")
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatalf("创建输出目录: %v", err)
	}

	var seq int64
	var seenMu sync.Mutex
	seen := map[string]int{}

	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		n := atomic.AddInt64(&seq, 1)

		// 文件名含路径与序号，便于区分同一轮的多次请求
		safePath := strings.NewReplacer("/", "_", "?", "_", "&", "_").Replace(r.URL.Path)
		name := fmt.Sprintf("%03d%s.json", n, safePath)
		full := filepath.Join(*outDir, name)

		if err := os.WriteFile(full, body, 0o644); err != nil {
			log.Printf("写入 %s 失败: %v", full, err)
		}

		// 记录请求头，供确认 wire_api 与鉴权形态
		hdr := map[string][]string(r.Header)
		hdrName := fmt.Sprintf("%03d%s.headers.json", n, safePath)
		if b, err := json.MarshalIndent(hdr, "", "  "); err == nil {
			_ = os.WriteFile(filepath.Join(*outDir, hdrName), b, 0o644)
		}

		seenMu.Lock()
		seen[r.URL.Path]++
		count := seen[r.URL.Path]
		seenMu.Unlock()

		log.Printf("#%d %s %s body=%dB headers=%d (path seen %d times)",
			n, r.Method, r.URL.Path, len(body), len(hdr), count)

		w.Header().Set("Content-Type", "application/json")

		if strings.Contains(r.URL.Path, "responses") {
			// codex 默认 stream:true 且要求 SSE。若按普通 JSON 返回，
			// codex 会报 "stream disconnected before completion" 并一直重试，
			// 永远走不到"回填工具结果"的下一轮 —— 那样就抓不到带工具调用的请求。
			//
			// 因此：请求要流式就返回**合法的 SSE 序列**（空正文、不发工具调用），
			// 让 codex 能正常结束这一轮。
			if wantsStream(body) {
				writeSSECompleted(w, n)
				return
			}
			writeJSONCompleted(w, n)
			return
		}
		// 其他路径（如 /v1/models）返回空对象，避免 codex 报错
		fmt.Fprint(w, `{}`)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handler)

	log.Printf("recordupstream listening on %s, writing to %s", *addr, *outDir)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
