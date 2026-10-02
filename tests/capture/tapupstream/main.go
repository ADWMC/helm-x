// tapupstream 是一个**透传式抓包代理**：把请求体落盘后**原样转发**到真实上游，
// 并把上游响应原样回给调用方。
//
// 与 recordupstream 的区别：
//   - recordupstream 返回固定响应（用于抓"第一轮"请求），无法驱动工具调用
//   - tapupstream 真实转发（用于抓"带工具结果的后续轮次"）
//
// 与 captureproxybody 的区别：后者同样转发，但本工具额外记录**响应**，
// 便于对照"哪些请求触发了拒绝/TAMPER"。
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:19002", "listen address")
	upstream := flag.String("upstream", "", "真实上游 base_url，如 https://example/v1")
	outDir := flag.String("out", ".", "记录目录")
	flag.Parse()

	if *upstream == "" {
		log.Fatal("必须指定 -upstream")
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatal(err)
	}

	var seq int64
	target := strings.TrimRight(*upstream, "/")

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		n := atomic.AddInt64(&seq, 1)

		// 落盘请求（含头，便于分析鉴权与 session）
		reqName := filepath.Join(*outDir, fmt.Sprintf("%03d-req.json", n))
		_ = os.WriteFile(reqName, body, 0o644)

		// 拼接目标 URL。
		//
		// codex 发的路径已含 /v1（如 /v1/responses），而 -upstream 也含 /v1。
		// 直接相加会得到 /v1/v1/responses（实测 404）。
		// 规则：若上游 base 的路径段已包含请求路径的开头，则不重复拼接。
		url := joinURL(target, r.URL.Path)
		if r.URL.RawQuery != "" {
			url += "?" + r.URL.RawQuery
		}
		req, err := http.NewRequest(r.Method, url, bytes.NewReader(body))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		// 原样转发全部请求头（含 Authorization / Session-Id / Accept）
		for k, vs := range r.Header {
			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}

		client := &http.Client{Timeout: 300 * time.Second}
		start := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("#%d 转发失败: %v", n, err)
			http.Error(w, err.Error(), 502)
			return
		}
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)

		respName := filepath.Join(*outDir, fmt.Sprintf("%03d-resp.txt", n))
		_ = os.WriteFile(respName, respBody, 0o644)

		log.Printf("#%d %s %s req=%dB resp=%dB status=%d elapsed=%dms",
			n, r.Method, r.URL.Path, len(body), len(respBody), resp.StatusCode,
			time.Since(start).Milliseconds())

		for k, vs := range resp.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBody)
	})

	fmt.Fprintf(os.Stderr, "tapupstream %s -> %s (out=%s)\n", *addr, target, *outDir)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

// joinURL 拼接上游地址与请求路径，避免 /v1 重复。
//
//	joinURL("https://h/v1", "/v1/responses") → "https://h/v1/responses"
//	joinURL("https://h",    "/v1/responses") → "https://h/v1/responses"
//	joinURL("https://h/v1", "/responses")    → "https://h/v1/responses"
func joinURL(base, path string) string {
	base = strings.TrimRight(base, "/")
	if path == "" {
		return base
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	// base 的路径部分
	idx := strings.Index(base, "://")
	hostEnd := len(base)
	if idx >= 0 {
		if p := strings.IndexByte(base[idx+3:], '/'); p >= 0 {
			hostEnd = idx + 3 + p
		}
	}
	basePath := strings.TrimRight(base[hostEnd:], "/")
	if basePath != "" && strings.HasPrefix(path, basePath+"/") {
		return base[:hostEnd] + path
	}
	if basePath != "" && path == basePath {
		return base[:hostEnd] + path
	}
	return base + path
}
