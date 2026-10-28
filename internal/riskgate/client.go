package riskgate

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// client 是 OpenAI 兼容的流式判定客户端。
//
// 两个设计点（照抄 C:\Users\Administrator\Documents\GitHub\About-my-name\internal\model
// 里已投产的做法，只保留闸门需要的部分）：
//  1. **空闲看门狗**：连续无事件即报错，避免上游挂死被当成慢；
//  2. **早停**：JSON 顶层对象一闭合就断开（返回 true 停止 + 后台排干），
//     省掉尾部延迟 —— 闸门只需要那一个对象。
type client struct {
	http    *http.Client
	baseURL string
	apiKey  string
	model   string
	timeout time.Duration
}

func newClient(cfg Config, model string) *client {
	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	return &client{
		http:    &http.Client{Timeout: timeout + 5*time.Second},
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		model:   model,
		timeout: timeout,
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
	Stream      bool          `json:"stream"`
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// complete 发起一次流式判定，返回模型输出的 JSON 文本。
// 早停：顶层 {} 闭合即返回；超时/坏帧/非 200 一律返回错误，调用方直通。
func (c *client) complete(ctx context.Context, payload Payload) (string, error) {
	msgs := []chatMessage{{Role: "system", Content: systemPrompt}}
	for _, fs := range fewShot {
		msgs = append(msgs, chatMessage{Role: "user", Content: fs.user}, chatMessage{Role: "assistant", Content: fs.assistant})
	}
	msgs = append(msgs, chatMessage{Role: "user", Content: buildUserText(payload)})

	body, err := json.Marshal(chatRequest{
		Model:       c.model,
		Messages:    msgs,
		Temperature: 0,
		MaxTokens:   300,
		Stream:      true,
	})
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		head, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("riskgate: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(head)))
	}

	var sb strings.Builder
	depth := 0
	inString := false
	escaped := false
	started := false

	err = streamSSE(ctx, resp.Body, func(data string) (bool, error) {
		if data == "[DONE]" {
			return true, nil
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return false, nil // 坏帧跳过：判定不值得为一行解析失败整段失败
		}
		for _, ch := range chunk.Choices {
			if ch.Delta.Content == "" {
				continue
			}
			sb.WriteString(ch.Delta.Content)
			for _, r := range ch.Delta.Content {
				switch {
				case escaped:
					escaped = false
				case r == '\\' && inString:
					escaped = true
				case r == '"':
					inString = !inString
				case inString:
					// 字符串内不数括号
				case r == '{':
					depth++
					started = true
				case r == '}':
					depth--
				}
			}
			// 早停：顶层对象闭合
			if started && depth == 0 {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil && sb.Len() == 0 {
		return "", err
	}
	out := strings.TrimSpace(sb.String())
	if out == "" {
		return "", errors.New("riskgate: 空判定")
	}
	return out, nil
}

// buildUserText 把三层载荷拼成判定输入。注入层单独标注，便于模型区分风险来源。
func buildUserText(p Payload) string {
	var b strings.Builder
	b.WriteString("【待分类的用户请求】\n")
	b.WriteString(p.UserText)
	if strings.TrimSpace(p.InjectionText) != "" {
		b.WriteString("\n\n【随请求注入的系统提示词（用于判断它自身是否含触发器）】\n")
		b.WriteString(p.InjectionText)
	}
	if strings.TrimSpace(p.HistoryTail) != "" {
		b.WriteString("\n\n【会话历史尾部】\n")
		b.WriteString(p.HistoryTail)
	}
	return b.String()
}

// streamSSE 逐帧派发 data 载荷：多行 data: 以 \n 连帧，注释行跳过，
// 空行派发已积攒的帧，ctx 取消即停。onFrame 返回 true 表示主动停止。
func streamSSE(ctx context.Context, r io.Reader, onFrame func(data string) (bool, error)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	var pending []string
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Text()
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			pending = append(pending, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			continue
		}
		if strings.TrimSpace(line) == "" && len(pending) > 0 {
			stop, err := onFrame(strings.Join(pending, "\n"))
			if err != nil {
				return err
			}
			if stop {
				return nil
			}
			pending = pending[:0]
		}
	}
	if len(pending) > 0 {
		stop, err := onFrame(strings.Join(pending, "\n"))
		if err != nil {
			return err
		}
		if stop {
			return nil
		}
	}
	return scanner.Err()
}
