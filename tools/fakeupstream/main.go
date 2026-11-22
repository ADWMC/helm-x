// Command fakeupstream 是一个最小的假上游，用来**看发出去的字节**。
//
// 用途：验证注入/部署语义的真实落点（`instructions` 是否被整段替换、input 是否被插 system），
// 不联网、不烧额度。每收到一个请求打印一行摘要，可供 shell 断言。
//
// 用法：
//
//	go run ./tools/fakeupstream -addr 127.0.0.1:1805 -exit-after 1
//	helmx proxy --listen 1806 --upstream http://127.0.0.1:1805/v1 &
//	curl -s -XPOST 127.0.0.1:1806/v1/responses -d @req.json
//
// 输出（stdout，一行一条）：
//
//	REQ #1 path=/v1/responses body=512 instructions=28451 hasOfficial=false systemMsgs=0
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:1805", "监听地址")
	exitAfter := flag.Int("exit-after", 0, "收到 N 个请求后退出（0 = 一直跑）")
	probe := flag.String("probe", "OFFICIAL-BASE", "在 instructions 里查找这个串")
	flag.Parse()

	var n atomic.Int64
	var srv *http.Server
	srv = &http.Server{Addr: *addr, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 32<<20))
		i := n.Add(1)

		var top struct {
			Instructions string          `json:"instructions"`
			Input        json.RawMessage `json:"input"`
		}
		_ = json.Unmarshal(body, &top)

		systemMsgs := strings.Count(string(top.Input), `"role":"system"`)
		fmt.Printf("REQ #%d path=%s body=%d instructions=%d hasProbe=%v systemMsgs=%d\n",
			i, r.URL.Path, len(body), len(top.Instructions),
			strings.Contains(top.Instructions, *probe), systemMsgs)

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_fake","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"fake-ok"}]}],"status":"completed"}`)
		if *exitAfter > 0 && int(i) >= *exitAfter {
			go func() { _ = srv.Close() }()
		}
	})}

	fmt.Fprintf(os.Stderr, "fakeupstream listening on %s\n", *addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "fakeupstream:", err)
		os.Exit(1)
	}
}
