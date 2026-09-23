package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed 批量向量化；返回顺序与输入一致。
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if c.embedURL == "" {
		return nil, fmt.Errorf("嵌入服务地址未配置")
	}
	body, err := json.Marshal(embedRequest{Model: c.embedModel, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("序列化嵌入请求失败: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.embedURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构造嵌入请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.embedKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.embedKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用嵌入服务失败: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("读取嵌入响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("嵌入服务返回 %d: %s", resp.StatusCode, truncate(string(respBody), 512))
	}
	var parsed embedResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("解析嵌入响应失败: %w", err)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("嵌入返回数量不匹配: 期望 %d 实际 %d", len(texts), len(parsed.Data))
	}
	vectors := make([][]float32, len(texts))
	for _, item := range parsed.Data {
		if item.Index < 0 || item.Index >= len(texts) {
			return nil, fmt.Errorf("嵌入返回序号越界: %d", item.Index)
		}
		vectors[item.Index] = item.Embedding
	}
	return vectors, nil
}
