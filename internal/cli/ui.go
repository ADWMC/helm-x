package cli

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ADWMC/helm-x/internal/proxy"
)

// runUI 实现 `helmx ui`：浏览器控制台。
//
// 这是 WebView2 不可用时的降级路径（RISKS.md R-6），
// 不是主界面 —— 主界面是 Wails 桌面窗口（RunGUI）。
//
// 职责：起一个极简 HTTP 服务，把运行时状态渲染成页面。
// 不做复杂交互：查看状态、看日志、看请求记录。写操作仍走 CLI。
func runUI(ctx context.Context, args []string, stdout io.Writer) error {
	port := 8090
	for i := 0; i < len(args); i++ {
		if args[i] == "--port" {
			v, err := nextArg(args, &i, "--port")
			if err != nil {
				return &ExitError{Code: 2, Err: err}
			}
			p, cerr := strconv.Atoi(v)
			if cerr != nil || p <= 0 || p > 65535 {
				return &ExitError{Code: 2, Err: fmt.Errorf("--port 需要 1-65535")}
			}
			port = p
		} else {
			return &ExitError{Code: 2, Err: fmt.Errorf("未知选项 %q", args[i])}
		}
	}

	rt := newRuntime(stdout)
	defer rt.Close()

	upstream := "(未启动)"
	if home, ok := rt.Home(); ok {
		if relay, err := home.RelayURL(); err == nil && relay != "" {
			upstream = relay
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		data := uiData{
			Upstream:  upstream,
			LogPath:   rt.Logger.Path(),
			ConfigURL: rt.Store.Path(),
		}
		if e := rt.Engine(); e != nil {
			st := e.Stats()
			data.Running = st.Running
			data.Listen = st.Listen
			data.Requests = st.Requests
			data.ByClass = st.ByClass
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = uiTemplate.Execute(w, data)
	})
	mux.HandleFunc("/api/log", func(w http.ResponseWriter, r *http.Request) {
		lines := rt.Logger.Tail(300)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte("{\"lines\":["))
		for i, l := range lines {
			if i > 0 {
				_, _ = w.Write([]byte(","))
			}
			fmt.Fprintf(w, "{\"ts\":%q,\"level\":%q,\"text\":%q}",
				l.TS.Format("15:04:05"), l.Level, l.Text)
		}
		_, _ = w.Write([]byte("]}"))
	})

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	fmt.Fprintf(stdout, "控制台: %s\n", url)
	fmt.Fprintln(stdout, "（这是降级界面；桌面窗口请直接运行 helmx）")

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()
	openBrowser(url)

	if err := srv.Serve(ln); err != nil && !strings.Contains(err.Error(), "Server closed") {
		return err
	}
	return nil
}

type uiData struct {
	Running   bool
	Listen    string
	Upstream  string
	Requests  uint64
	ByClass   map[proxy.Class]uint64
	LogPath   string
	ConfigURL string
}

var uiTemplate = template.Must(template.New("ui").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>helm-x 控制台</title>
<style>
 body{margin:0;font:14px/1.6 system-ui,"Microsoft YaHei",sans-serif;background:#0d0d0d;color:#fff}
 .wrap{max-width:960px;margin:0 auto;padding:32px 24px}
 h1{font-size:20px;margin:0 0 4px}
 .sub{color:#9b9b9b;font-size:13px;margin-bottom:24px}
 .stats{display:grid;grid-template-columns:repeat(auto-fit,minmax(150px,1fr));gap:1px;
        background:#3c3c3c;border:1px solid #3c3c3c;border-radius:12px;overflow:hidden;margin-bottom:24px}
 .stat{background:#1a1a1a;padding:16px}
 .stat span{display:block;color:#9b9b9b;font-size:11px}
 .stat strong{display:block;margin-top:6px;font-size:20px}
 .ok{color:#10a37f}.bad{color:#ef4146}.warn{color:#f5a623}
 pre{background:#1a1a1a;border:1px solid #3c3c3c;border-radius:12px;padding:16px;
     font:11.5px/1.6 ui-monospace,Consolas,monospace;max-height:420px;overflow:auto;white-space:pre-wrap}
 .meta{color:#6e6e6e;font:11px/1.8 ui-monospace,Consolas,monospace;margin-top:20px}
</style></head><body><div class="wrap">
<h1>helm-x 控制台</h1>
<div class="sub">降级界面 —— 完整界面请运行 helmx（桌面窗口）</div>
<div class="stats">
  <div class="stat"><span>代理</span><strong class="{{if .Running}}ok{{else}}bad{{end}}">{{if .Running}}运行中{{else}}已停止{{end}}</strong></div>
  <div class="stat"><span>监听</span><strong>{{if .Listen}}{{.Listen}}{{else}}—{{end}}</strong></div>
  <div class="stat"><span>请求数</span><strong>{{.Requests}}</strong></div>
  <div class="stat"><span>上游</span><strong style="font-size:12px">{{.Upstream}}</strong></div>
</div>
{{if .ByClass}}<div class="stats">{{range $k,$v := .ByClass}}
  <div class="stat"><span>{{$k}}</span><strong>{{$v}}</strong></div>{{end}}</div>{{end}}
<h1 style="font-size:15px;margin:24px 0 10px">最近日志</h1>
<pre id="log">加载中…</pre>
<div class="meta">日志文件: {{.LogPath}}<br>配置文件: {{.ConfigURL}}</div>
<script>
async function tick(){
  try{
    const r = await fetch('/api/log'); const d = await r.json();
    document.getElementById('log').textContent = d.lines.map(l=>l.ts+' '+l.text).join('\n') || '(暂无日志)';
  }catch(e){}
}
tick(); setInterval(tick, 2000);
</script>
</div></body></html>`))
