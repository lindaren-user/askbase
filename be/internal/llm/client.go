// Package llm 实现 OpenAI 兼容协议客户端：Embedding、文本生成、视觉理解与 SSE 流式对话。
package llm

import (
	"net/http"
	"strings"
	"time"
)

// Client OpenAI 兼容服务客户端（对话与嵌入可指向不同服务）。
type Client struct {
	chatURL    string
	chatKey    string
	chatModel  string
	embedURL   string
	embedKey   string
	embedModel string
	http       *http.Client
}

// ChatMessage 对话消息。
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// New 创建客户端；chatAPIURL 与 embedAPIURL 均为 OpenAI 兼容 base URL。
func New(chatAPIURL, chatAPIKey, chatModel, embedAPIURL, embedAPIKey, embedModel string) *Client {
	return &Client{
		chatURL:    chatCompletionsURL(chatAPIURL),
		chatKey:    chatAPIKey,
		chatModel:  chatModel,
		embedURL:   normalizeBaseURL(embedAPIURL),
		embedKey:   embedAPIKey,
		embedModel: embedModel,
		http:       &http.Client{Timeout: 120 * time.Second},
	}
}

// chatCompletionsURL 根据 base URL 构造对话补全接口地址。
func chatCompletionsURL(raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return raw
	}
	return raw + "/chat/completions"
}

// normalizeBaseURL 去掉尾斜杠，便于拼接子路径。
func normalizeBaseURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}
