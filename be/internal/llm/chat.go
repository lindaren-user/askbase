package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type chatRequest struct {
	Model     string        `json:"model"`
	Messages  []ChatMessage `json:"messages"`
	Stream    bool          `json:"stream"`
	MaxTokens int           `json:"max_tokens,omitempty"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message ChatMessage `json:"message"`
	} `json:"choices"`
}

// Complete 发起非流式文本生成，供需要完整 JSON 等结果的内部任务使用。
func (c *Client) Complete(ctx context.Context, messages []ChatMessage, maxTokens int) (string, error) {
	if c.chatURL == "" {
		return "", fmt.Errorf("对话服务地址未配置")
	}
	body, err := json.Marshal(chatRequest{Model: c.chatModel, Messages: messages, Stream: false, MaxTokens: maxTokens})
	if err != nil {
		return "", fmt.Errorf("序列化对话请求失败: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.chatURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("构造对话请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.chatKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.chatKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("调用对话服务失败: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", fmt.Errorf("读取对话响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s", providerErrorMessage(resp.StatusCode, respBody))
	}
	var parsed chatCompletionResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("解析对话响应失败: %w", err)
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("对话响应为空")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}
