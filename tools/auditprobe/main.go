// Command auditprobe 是审查用的一次性探针：把 codexcfg / proxy 的边界行为
// 打成可复现的输出（不写用户目录、不发真实上游）。
//
// 用法：go run ./tools/auditprobe
//
// 它存在的意义是让审查结论可复现 —— 每个 T*/P* 段落对应报告里的编号。
// 用完可整目录删除，不影响构建（tools/ 不参与 main 包）。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ADWMC/helm-x/internal/assets"
	"github.com/ADWMC/helm-x/internal/codexcfg"
	"github.com/ADWMC/helm-x/internal/proxy"
	"github.com/ADWMC/helm-x/internal/tamper"

	"github.com/ADWMC/helm-x/internal/riskgate"
)

func hdr(s string) { fmt.Printf("\n=== %s ===\n", s) }

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// probeCfg 是 proxy.ConfigProvider 的最小实现，只提供 TAMPER 规则。
type probeCfg struct{ tm proxy.TamperMatcher }

func (c probeCfg) PromptInstruction() string   { return "" }
func (c probeCfg) InjectEvery() int            { return 1 }
func (c probeCfg) FallbackUserAgent() string   { return "" }
func (c probeCfg) Rewriter() proxy.Rewriter    { return nil }
func (c probeCfg) Gate() *riskgate.Gate        { return nil }
func (c probeCfg) Tamper() proxy.TamperMatcher { return c.tm }
func (c probeCfg) Retry() proxy.RetryOptions   { return proxy.RetryOptions{} }

func main() {
	// ---------------- codexcfg ----------------
	hdr("T1 TOML 多行数组（合法配置）")
	raw1 := "model_provider = \"r\"\nnotify = [\n  \"a\",\n  \"b\",\n]\n\n[model_providers.r]\nname = \"r\"\nbase_url = \"https://up.example/v1\"\n"
	if _, err := codexcfg.ParseDoc([]byte(raw1)); err != nil {
		fmt.Printf("ParseDoc -> 失败: %v\n", err)
	} else {
		fmt.Println("ParseDoc -> 成功")
	}

	hdr("T2 base_url 点修改 + 其余行字节保真")
	raw2 := "# head comment\nmodel_provider = \"r\"\n\n[model_providers.r]\nname = \"r\"\nbase_url = \"https://up.example/v1\" # note \"q\"\nwire_api = \"responses\"\n"
	d, err := codexcfg.ParseDoc([]byte(raw2))
	if err != nil {
		fmt.Println("解析失败:", err)
	} else if err := d.SetString([]string{"model_providers", "r"}, "base_url", "http://127.0.0.1:1800/v1"); err != nil {
		fmt.Println("SetString 失败:", err)
	} else {
		out := string(d.Bytes())
		fmt.Printf("输出:\n%s---\n行数不变: %v\n", out, strings.Count(out, "\n") == strings.Count(raw2, "\n"))
	}

	hdr("T3 值里含 # 的字符串")
	raw3 := "model_provider = \"r\"\n\n[model_providers.r]\nbase_url = \"https://relay.example/v1#frag\"\n"
	if d3, err := codexcfg.ParseDoc([]byte(raw3)); err != nil {
		fmt.Println("解析失败:", err)
	} else if v, ok := d3.GetString([]string{"model_providers", "r"}, "base_url"); ok {
		fmt.Printf("取到 base_url = %q\n", v)
	} else {
		fmt.Println("取不到 base_url")
	}

	hdr("T4 CRLF 文件插入顶层键")
	raw4 := "model_provider = \"r\"\r\n\r\n[model_providers.r]\r\nbase_url = \"https://up.example/v1\"\r\n"
	if d4, err := codexcfg.ParseDoc([]byte(raw4)); err != nil {
		fmt.Println("解析失败:", err)
	} else {
		_ = d4.InsertTopLevelRaw("tool_output_token_limit", []byte("8000"))
		out := d4.Bytes()
		bare := strings.Count(string(out), "\n") - strings.Count(string(out), "\r\n")
		fmt.Printf("CRLF 保留: %v  裸 LF 数量: %d\n", strings.Contains(string(out), "\r\n"), bare)
		fmt.Printf("首行: %q\n", strings.SplitN(string(out), "\r\n", 2)[0])
	}

	hdr("T5 [[array of tables]]")
	if _, err := codexcfg.ParseDoc([]byte("[[plugins]]\nname = \"x\"\n")); err != nil {
		fmt.Printf("ParseDoc -> 拒绝: %v\n", err)
	} else {
		fmt.Println("ParseDoc -> 接受（不应发生）")
	}

	hdr("T6 行内表 { }")
	if _, err := codexcfg.ParseDoc([]byte("history = { persistence = \"save-all\" }\n")); err != nil {
		fmt.Printf("ParseDoc -> 拒绝: %v\n", err)
	} else {
		fmt.Println("ParseDoc -> 接受，值按原始字节保留")
	}

	// ---------------- proxy ----------------
	hdr("P1 上游 URL 的 query 是否保留")
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"output":[{"content":[{"text":"ok"}]}]}`)
	}))
	defer srv.Close()
	if up, err := proxy.NewUpstream(srv.URL + "/v1?api-version=2024-10-21"); err != nil {
		fmt.Println("NewUpstream 失败:", err)
	} else {
		_ = up.Post(context.Background(), "/v1/responses", []byte(`{}`), http.Header{})
		fmt.Printf("上游实际收到: %s\n", gotURI)
	}

	hdr("P2 重试次数（MaxRetries=2，上游恒 500）")
	var hits int32
	srv500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(500)
		fmt.Fprint(w, `{"error":"boom"}`)
	}))
	defer srv500.Close()
	up2, _ := proxy.NewUpstream(srv500.URL)
	a := up2.PostWithRetry(context.Background(), "/v1/responses", []byte(`{}`), http.Header{},
		proxy.RetryOptions{Enabled: true, MaxRetries: 2, DelaySeconds: 0}, nil)
	fmt.Printf("上游请求数=%d 最终 status=%d\n", atomic.LoadInt32(&hits), a.Status)

	hdr("P3 200 + SSE 里的拒答如何判定")
	sse := "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"I'm sorry, but I can't help with that.\"}\n\n"
	st := proxy.Classify(proxy.ClassifyInput{
		Status: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: []byte(sse), Complete: true,
		IsRefusal: func(s string) bool { return strings.Contains(s, "I'm sorry") },
	})
	fmt.Printf("Class=%s Reason=%s\n", st.Class, st.Reason)

	hdr("P4 BuildCleanSession 保留/丢弃哪些字段")
	body := `{"model":"gpt-5.6","stream":true,"store":false,"instructions":"sys","include":["a"],"prompt_cache_key":"k","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}],"tools":[{"type":"function","name":"f"}],"reasoning":{"effort":"high"}}`
	if v, err := proxy.ParseRequest("/v1/responses", []byte(body)); err != nil {
		fmt.Println("解析失败:", err)
	} else if clean, err := v.BuildCleanSession("hello"); err != nil {
		fmt.Println("构建失败:", err)
	} else {
		var m map[string]any
		_ = json.Unmarshal(clean, &m)
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		fmt.Printf("保留键: %v\n合法 JSON: %v\n", keys, json.Valid(clean))
	}

	hdr("T7 拒绝规则对正常技术回答的误判")
	tm, errs := tamper.New(assets.TamperRules())
	fmt.Printf("规则数=%d 编译错误=%d\n", tm.Len(), len(errs))
	benign := []string{
		"I am unable to reproduce this bug on my machine, could you share the logs?",
		"This is a dangerous operation — always back up the database first.",
		"The parameter is not allowed to be null.",
		"The function terminates subsequent execution, so add a lock before calling it.",
		"该函数会终止后续执行，建议先加锁。",
		"The exploit is dangerous when left uncontrolled in kernel space.",
		"We don't support Windows 7 anymore.",
		"这个错误是数组越界导致的，访问 arr[10] 会读到未定义内存。",
		"This violates the single-responsibility principle.",
		"抱歉，我无法访问外部网络，请把文件内容贴过来。",
		"Index out of range: the slice has length 3 but you accessed index 5.",
		"Buffer overflow is illegal access to memory outside the allocated region.",
	}
	fp := 0
	for _, s := range benign {
		if rule := tm.Match(s); rule != "" {
			fp++
			fmt.Printf("[FP] %-72s <= %s\n", trunc(s, 70), trunc(rule, 60))
		}
	}
	fmt.Printf("误判 %d/%d\n", fp, len(benign))

	hdr("T8 端到端：良性回答被前置 marker + 多打一次上游")
	var hits8 int32
	srv8 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits8, 1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"output":[{"type":"message","content":[{"type":"output_text","text":"The exploit is dangerous when left uncontrolled."}]}]}`)
	}))
	defer srv8.Close()
	eng, err := proxy.New(proxy.Options{Listen: "127.0.0.1:0", Upstream: srv8.URL, Config: probeCfg{tm: tm}})
	if err != nil {
		fmt.Println("New 失败:", err)
	} else {
		ctx, cancel := context.WithCancel(context.Background())
		go func() { _ = eng.Start(ctx) }()
		deadline := time.Now().Add(3 * time.Second)
		for !eng.Running() && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		reqBody := `{"model":"gpt-5.6","stream":false,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"why is this dangerous"}]}]}`
		resp, perr := http.Post("http://"+eng.Addr()+"/v1/responses", "application/json", strings.NewReader(reqBody))
		if perr != nil {
			fmt.Println("请求失败:", perr)
		} else {
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			fmt.Printf("上游收到请求数=%d\n", atomic.LoadInt32(&hits8))
			fmt.Printf("客户端收到: %s\n", trunc(string(raw), 220))
		}
		cancel()
		_ = eng.Stop()
	}

	hdr("P5 重复注入的幂等性")
	if v, err := proxy.ParseRequest("/v1/responses", []byte(body)); err != nil {
		fmt.Println("解析失败:", err)
	} else {
		ins := "You are a CTF agent. " + strings.Repeat("x", 80)
		b1, ok1, e1 := v.InjectSystem(ins)
		v2, _ := proxy.ParseRequest("/v1/responses", b1)
		b2, ok2, e2 := v2.InjectSystem(ins)
		fmt.Printf("第一次 ok=%v err=%v len=%d\n第二次 ok=%v err=%v 长度不变=%v\n", ok1, e1, len(b1), ok2, e2, len(b1) == len(b2))
		fmt.Printf("注入后合法 JSON: %v\n", json.Valid(b1))
	}
}
