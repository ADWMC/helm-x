// Command uiserve 把前端构建产物作为静态站点提供给内网访问。
//
// 用途：把界面推给局域网里的其他设备看效果，**不接后端**。
// 前端在没有 Wails 绑定时会自行退化为演示模式并在页面顶部提示，
// 因此不会有人误以为可以远程操作代理或读写 codex 配置。
//
// 与 helmx ui 的区别：
//
//	helmx ui        有后端（能启停代理、改配置），只监听 127.0.0.1
//	uiserve（本工具）无后端（纯静态壳），可监听内网
//
// 这是刻意的隔离：把「能改配置的界面」限制在本机，
// 内网只暴露不含任何能力的静态预览。
//
// 用法：
//
//	go run ./tools/uiserve -dir frontend/dist -addr 0.0.0.0:8090
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	dir := flag.String("dir", "frontend/dist", "前端构建产物目录")
	addr := flag.String("addr", "0.0.0.0:8090", "监听地址")
	flag.Parse()

	root, err := filepath.Abs(*dir)
	if err != nil {
		log.Fatalf("解析目录失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "index.html")); err != nil {
		log.Fatalf("在 %s 找不到 index.html。先执行：cd frontend && npm run build", root)
	}

	mux := http.NewServeMux()
	mux.Handle("/", spaHandler(root))

	// 内网访问时用真实地址提示，避免打印 0.0.0.0 让人不知道怎么访问
	urls := accessURLs(*addr)
	fmt.Printf("前端预览：%s\n", root)
	fmt.Println("可访问地址：")
	for _, u := range urls {
		fmt.Printf("  %s\n", u)
	}
	fmt.Println()
	fmt.Println("提示：这是纯静态预览，未连接后端。")
	fmt.Println("      页面会显示「浏览器预览模式」横幅，操作按钮不可用。")
	fmt.Println("      需要真实功能请在本机运行 helmx（代理只监听 127.0.0.1）。")
	fmt.Println()
	fmt.Println("按 Ctrl+C 停止")

	srv := &http.Server{
		Addr:              *addr,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

// spaHandler 提供静态文件，未命中的路径回落到 index.html（前端路由需要）。
func spaHandler(root string) http.Handler {
	fs := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := filepath.Join(root, filepath.Clean("/"+r.URL.Path))
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			// 构建产物带内容哈希，可长期缓存
			if strings.Contains(r.URL.Path, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			fs.ServeHTTP(w, r)
			return
		}

		// Wails 绑定通道：本服务不提供后端，必须明确回 404 JSON。
		//
		// 【为何重要】若回落到 index.html，前端拿到的是一段 HTML，
		// JSON.parse 会失败并弹出 "Unexpected token '<'" 之类的原始报错 ——
		// 用户看到的是难懂的解析错误，而不是"演示模式"提示。
		// 返回结构化的 404 后，绑定调用会正常拒绝，
		// 前端的 demoGuard 就能按预期退化为演示数据。
		if strings.HasPrefix(r.URL.Path, "/wails/") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"uiserve 不提供后端绑定"}`))
			return
		}

		// 前端路由（如 /requests）直接访问时回落到 index.html
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, filepath.Join(root, "index.html"))
	})
}

// logRequests 记录访问，便于确认是否真的有人连上。
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		// 静态资源请求量大且无信息量，只记页面请求
		if !strings.Contains(r.URL.Path, "/assets/") {
			log.Printf("%s %s ← %s (%s)", r.Method, r.URL.Path, r.RemoteAddr, time.Since(start).Round(time.Millisecond))
		}
	})
}

// accessURLs 列出实际的访问地址。监听 0.0.0.0 时展开为本机各网卡地址。
func accessURLs(addr string) []string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return []string{"http://" + addr}
	}
	if host != "0.0.0.0" && host != "::" {
		return []string{fmt.Sprintf("http://%s:%s", host, port)}
	}

	out := []string{fmt.Sprintf("http://127.0.0.1:%s", port)}
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}
			out = append(out, fmt.Sprintf("http://%s:%s", ipnet.IP, port))
		}
	}
	return out
}
