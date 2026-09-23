package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type chatStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// StreamEvent 流式事件：Content 为增量文本；Err 非空表示失败；Done 标记正常结束。
type StreamEvent struct {
	Content      string
	FinishReason string
	Done         bool
	Err          error
}

// ChatStream 发起 SSE 流式对话；返回只读 channel，流结束或出错后关闭。
// 出错时先投递 error 事件再关闭。调用方须读完 channel 以释放连接。
func (c *Client) ChatStream(ctx context.Context, messages []ChatMessage, maxTokens int) <-chan StreamEvent {
	events := make(chan StreamEvent, 16)
	go func() {
		defer close(events)
		if c.chatURL == "" {
			events <- StreamEvent{Err: fmt.Errorf("对话模型地址未配置")}
			return
		}
		body, err := json.Marshal(chatRequest{
			Model:     c.chatModel,
			Messages:  messages,
			Stream:    true,
			MaxTokens: maxTokens,
		})
		if err != nil {
			events <- StreamEvent{Err: fmt.Errorf("序列化对话请求失败: %w", err)}
			return
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.chatURL, bytes.NewReader(body))
		if err != nil {
			events <- StreamEvent{Err: fmt.Errorf("构造对话请求失败: %w", err)}
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		if c.chatKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.chatKey)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			events <- StreamEvent{Err: fmt.Errorf("调用对话服务失败: %w", err)}
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			events <- StreamEvent{Err: fmt.Errorf("对话服务返回 %d: %s", resp.StatusCode, truncate(string(respBody), 512))}
			return
		}
		if err := c.consumeSSE(resp.Body, events); err != nil {
			events <- StreamEvent{Err: err}
		}
	}()
	return events
}

// consumeSSE 逐行解析 SSE 数据帧，并校验流必须以结束标记正常终止。
func (c *Client) consumeSSE(body io.Reader, events chan<- StreamEvent) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	var dataLines []string
	flush := func() (bool, error) {
		if len(dataLines) == 0 {
			return true, nil
		}
		data := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		if data == "[DONE]" {
			events <- StreamEvent{Done: true}
			return false, nil
		}
		var chunk chatStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return false, fmt.Errorf("解析流式响应数据失败: %w", err)
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				events <- StreamEvent{Content: choice.Delta.Content}
			}
			if choice.FinishReason != nil && *choice.FinishReason != "" {
				events <- StreamEvent{Done: true, FinishReason: *choice.FinishReason}
				return false, nil
			}
		}
		return true, nil
	}
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			more, err := flush()
			if err != nil {
				return err
			}
			if !more {
				return nil
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("读取流式响应失败: %w", err)
	}
	if len(dataLines) > 0 {
		more, err := flush()
		if err != nil {
			return err
		}
		if !more {
			return nil
		}
	}
	return fmt.Errorf("流式响应提前结束: 未收到结束标记")
}
