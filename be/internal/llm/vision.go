package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const imageDescribePrompt = "转录图中全部文字；若是图表请说明坐标轴与主要数据；否则用一两句话描述画面内容。不要编造看不清的内容。"

type visionChatRequest struct {
	Model     string              `json:"model"`
	Messages  []visionChatMessage `json:"messages"`
	MaxTokens int                 `json:"max_tokens,omitempty"`
}

type visionChatMessage struct {
	Role    string              `json:"role"`
	Content []visionContentPart `json:"content"`
}

type visionContentPart struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *visionImageURL `json:"image_url,omitempty"`
}

type visionImageURL struct {
	URL string `json:"url"`
}

type visionChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// DescribeImage 用视觉模型给图片生成文字描述。
func (c *Client) DescribeImage(ctx context.Context, mime string, image []byte, prompt string) (string, error) {
	if c.chatURL == "" {
		return "", fmt.Errorf("视觉模型地址未配置")
	}
	if len(image) == 0 {
		return "", fmt.Errorf("图片为空")
	}
	if mime == "" {
		mime = "image/png"
	}
	if strings.TrimSpace(prompt) == "" {
		prompt = imageDescribePrompt
	}
	body, err := json.Marshal(visionChatRequest{
		Model: c.chatModel,
		Messages: []visionChatMessage{{
			Role: "user",
			Content: []visionContentPart{
				{Type: "text", Text: prompt},
				{Type: "image_url", ImageURL: &visionImageURL{URL: "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(image)}},
			},
		}},
		MaxTokens: 800,
	})
	if err != nil {
		return "", fmt.Errorf("序列化视觉请求失败: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.chatURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("构造视觉请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.chatKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.chatKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("调用视觉服务失败: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("读取视觉响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s", providerErrorMessage(resp.StatusCode, respBody))
	}
	var parsed visionChatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("解析视觉响应失败: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("视觉响应为空")
	}
	// 自动模型路由（比如 openrouter/free）或视觉模型误配为内容安全模型时，可能返回 "User Safety: safe"；
	// 这不是图片识别结果，上层若不校验会将其作为描述写入形如 "[图片] User Safety: safe" 的分块。
	text := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if text == "" {
		return "", fmt.Errorf("视觉响应为空")
	}
	return text, nil
}
