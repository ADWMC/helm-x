package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxBodyBytes 与旧版一致（proxy.cpp:706）：超过即判失败，避免 OOM。
const maxBodyBytes = 16 << 20

// RetryOptions 是上游重试策略。
//
// 语义与旧版保持一致（proxy.cpp:720-793），这是兼容性承诺的一部分：
//   - MaxRetries 是**额外**尝试次数；0 表示无限
//   - 固定间隔，不使用上游的 Retry-After
//   - 4xx 不重试，**除了 408 和 429**（这两个是瞬时的）
type RetryOptions struct {
	Enabled      bool
	MaxRetries   int
	DelaySeconds int
}

// DefaultRetryOptions 与旧版默认值一致。
func DefaultRetryOptions() RetryOptions {
	return RetryOptions{Enabled: true, MaxRetries: 10, DelaySeconds: 3}
}

// Attempt 是一次上游请求的结果。
type Attempt struct {
	Status   int
	Header   http.Header
	Body     []byte
	Complete bool   // 是否完整读完
	Stage    string // 失败阶段，用于日志
	Err      error
}

// Upstream 是上游转发客户端。
type Upstream struct {
	base    *url.URL
	client  *http.Client
	auth    string // 由调用方按请求提供
	fwdKeys []string
}

// NewUpstream 解析上游地址并构造客户端。
func NewUpstream(rawURL string) (*Upstream, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("上游地址无法解析: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("上游地址缺少 scheme 或 host: %q", rawURL)
	}
	return &Upstream{
		base: u,
		client: &http.Client{
			// 不设总超时：流式响应可能很长，超时由 ctx 控制
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				TLSHandshakeTimeout:   30 * time.Second,
				ResponseHeaderTimeout: 300 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
				MaxIdleConns:          32,
				IdleConnTimeout:       90 * time.Second,
			},
		},
		fwdKeys: []string{
			"Authorization",
			"Accept",
			"Session-Id", "Thread-Id",
			"X-Client-Request-Id", "X-Codex-Installation-Id",
			"X-Codex-Window-Id", "X-Codex-Turn-Metadata",
			"X-Codex-Beta-Features",
			"X-Openai-Internal-Codex-Responses-Lite",
			"OpenAI-Beta",
			"Originator", "User-Agent",
		},
	}, nil
}

// BaseURL 返回上游地址。
func (u *Upstream) BaseURL() string { return u.base.String() }

// resolve 把入站路径映射到上游完整 URL。
//
// codex 发的路径已含 /v1（如 /v1/responses），上游 base 也常含 /v1。
// 直接拼接会得到 /v1/v1/responses —— 实测会 404。
func (u *Upstream) resolve(path string) string {
	basePath := strings.TrimRight(u.base.Path, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if basePath != "" && (strings.HasPrefix(path, basePath+"/") || path == basePath) {
		return u.base.Scheme + "://" + u.base.Host + path
	}
	return u.base.Scheme + "://" + u.base.Host + basePath + path
}

// Post 发送一次请求（不重试），把响应完整读入内存。
//
// 用于非流式、以及补救阶段需要完整内容再决策的场景。
func (u *Upstream) Post(ctx context.Context, path string, body []byte, in http.Header) Attempt {
	req, err := u.newRequest(ctx, path, body, in)
	if err != nil {
		return Attempt{Stage: "build", Err: err, Status: 502}
	}
	req.Header.Set("Accept", "application/json")

	resp, err := u.client.Do(req)
	if err != nil {
		return Attempt{Stage: "send", Err: err, Status: 502}
	}
	defer resp.Body.Close()

	// 读取上限保护
	limited := io.LimitReader(resp.Body, maxBodyBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return Attempt{Stage: "read", Err: err, Status: 502, Header: resp.Header}
	}
	if len(data) > maxBodyBytes {
		return Attempt{Stage: "response_too_large", Status: resp.StatusCode,
			Header: resp.Header, Err: fmt.Errorf("响应超过 %d 字节", maxBodyBytes)}
	}
	return Attempt{
		Status:   resp.StatusCode,
		Header:   resp.Header,
		Body:     data,
		Complete: true,
	}
}

// PostStream 发送请求并返回未读取的响应，供流式处理。
// 调用方负责关闭 resp.Body。
func (u *Upstream) PostStream(ctx context.Context, path string, body []byte, in http.Header) (*http.Response, error) {
	req, err := u.newRequest(ctx, path, body, in)
	if err != nil {
		return nil, err
	}
	if in == nil || in.Get("Accept") == "" {
		req.Header.Set("Accept", "text/event-stream")
	}
	return u.client.Do(req)
}

func (u *Upstream) newRequest(ctx context.Context, path string, body []byte, in http.Header) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.resolve(path), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for _, k := range u.fwdKeys {
		if in == nil {
			break
		}
		if v := in.Get(k); v != "" {
			req.Header.Set(k, v)
		}
	}
	return req, nil
}

// shouldRetry 判断一次失败是否值得重试。
//
// 与旧版 should_retry（proxy.cpp:720-728）语义一致：
//   - 没读完 → 重试
//   - 4xx（除 408/429）→ 不重试（重放同样的请求不会变好）
//   - 非 2xx 或空响应 → 重试
func shouldRetry(a Attempt) bool {
	if !a.Complete {
		return true
	}
	if a.Status >= 400 && a.Status < 500 && a.Status != 408 && a.Status != 429 {
		return false
	}
	return a.Status < 200 || a.Status >= 300 || len(bytes.TrimSpace(a.Body)) == 0
}

// retryReason 给出重试原因，写入日志。
func retryReason(a Attempt) string {
	if !a.Complete {
		if a.Stage != "" {
			return a.Stage
		}
		return "响应不完整"
	}
	if a.Status < 200 || a.Status >= 300 {
		return "HTTP " + itoa(a.Status)
	}
	return "空响应"
}

// PostWithRetry 按策略重试，直到成功或耗尽。
//
// 返回 (attempt, exhausted)：exhausted 为 true 表示重试次数已用尽。
func (u *Upstream) PostWithRetry(ctx context.Context, path string, body []byte,
	in http.Header, opts RetryOptions, onRetry func(n int, reason string)) Attempt {

	attemptNum := 1
	for {
		a := u.Post(ctx, path, body, in)
		if !shouldRetry(a) {
			return a
		}

		available := opts.Enabled && (opts.MaxRetries == 0 || attemptNum <= opts.MaxRetries)
		if !available {
			return a
		}
		if onRetry != nil {
			onRetry(attemptNum, retryReason(a))
		}
		if !sleepCtx(ctx, time.Duration(opts.DelaySeconds)*time.Second) {
			return a // 被取消
		}
		attemptNum++
	}
}

// sleepCtx 可被取消的等待。
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
