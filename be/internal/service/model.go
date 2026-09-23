package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"askbase/be/internal/llm"
	"askbase/be/internal/model"
)

// ModelService 自定义模型（BYOK）连通性测试；密钥由前端随请求携带，服务端不持久化。
type ModelService struct{}

// NewModelService 创建模型服务。
func NewModelService() *ModelService {
	return &ModelService{}
}

// ValidateChoice 校验对话请求的模型覆盖参数；official/空为合法（用服务端默认）。
func ValidateChoice(choice *model.ChatModelChoice) error {
	if choice == nil || choice.Source == "" || choice.Source == model.ModelSourceOfficial {
		return nil
	}
	if choice.Source != model.ModelSourceCustom {
		return fmt.Errorf("%w: 未知的模型来源", ErrInvalidInput)
	}
	if strings.TrimSpace(choice.ModelID) == "" ||
		strings.TrimSpace(choice.APIURL) == "" ||
		strings.TrimSpace(choice.APIKey) == "" {
		return fmt.Errorf("%w: 自定义模型需填写模型 ID、接口前缀与 API Key", ErrInvalidInput)
	}
	return nil
}

// Test 向自定义模型发一条短请求，确认接口可连通。
func (s *ModelService) Test(ctx context.Context, req model.TestModelRequest) (model.TestModelResponse, error) {
	if err := ValidateChoice(&model.ChatModelChoice{
		Source:  model.ModelSourceCustom,
		ModelID: req.ModelID,
		APIURL:  req.APIURL,
		APIKey:  req.APIKey,
	}); err != nil {
		return model.TestModelResponse{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	body, err := json.Marshal(map[string]any{
		"model":      strings.TrimSpace(req.ModelID),
		"max_tokens": 8,
		"messages": []map[string]string{
			{"role": "user", "content": "Reply with OK only."},
		},
	})
	if err != nil {
		return model.TestModelResponse{}, fmt.Errorf("序列化测试请求失败: %w", err)
	}
	payload, err := postJSON(ctx, chatCompletionsURL(req.APIURL), req.APIKey, body, 1<<20)
	if err != nil {
		return model.TestModelResponse{}, err
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return model.TestModelResponse{}, fmt.Errorf("%w: 响应不是有效的对话结果", ErrInvalidInput)
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return model.TestModelResponse{}, fmt.Errorf("%w: 模型测试响应为空", ErrAIUnavailable)
	}
	return model.TestModelResponse{OK: true, Message: "连接成功"}, nil
}

// TestEmbed 向自定义嵌入服务发一条短请求，验证连通性并回报向量维度。
func (s *ModelService) TestEmbed(ctx context.Context, req model.TestEmbedRequest) (model.TestModelResponse, error) {
	if strings.TrimSpace(req.ModelID) == "" || strings.TrimSpace(req.APIURL) == "" {
		return model.TestModelResponse{}, fmt.Errorf("%w: 自定义嵌入模型需填写模型 ID 与接口前缀", ErrInvalidInput)
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	body, err := json.Marshal(map[string]any{
		"model": strings.TrimSpace(req.ModelID),
		"input": []string{"ping"},
	})
	if err != nil {
		return model.TestModelResponse{}, fmt.Errorf("序列化测试请求失败: %w", err)
	}
	url := strings.TrimRight(strings.TrimSpace(req.APIURL), "/") + "/embeddings"
	payload, err := postJSON(ctx, url, req.APIKey, body, 8<<20)
	if err != nil {
		return model.TestModelResponse{}, err
	}

	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil || len(parsed.Data) == 0 || len(parsed.Data[0].Embedding) == 0 {
		return model.TestModelResponse{}, fmt.Errorf("%w: 嵌入响应为空或格式不符", ErrAIUnavailable)
	}
	return model.TestModelResponse{OK: true, Message: fmt.Sprintf("连接成功，向量维度 %d", len(parsed.Data[0].Embedding))}, nil
}

// 1x1 透明 PNG，用于视觉模型连通性测试。
var visionProbePNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
	0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

// TestVision 向自定义视觉模型发一张 1x1 图，验证连通性。
func (s *ModelService) TestVision(ctx context.Context, req model.TestModelRequest) (model.TestModelResponse, error) {
	if strings.TrimSpace(req.ModelID) == "" || strings.TrimSpace(req.APIURL) == "" {
		return model.TestModelResponse{}, fmt.Errorf("%w: 自定义视觉模型需填写模型 ID 与接口前缀", ErrInvalidInput)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client := llm.New(req.APIURL, req.APIKey, req.ModelID, "", "", "")
	if _, err := client.DescribeImage(ctx, "image/png", visionProbePNG, "Reply with OK only."); err != nil {
		return model.TestModelResponse{OK: false, Message: err.Error()}, nil
	}
	return model.TestModelResponse{OK: true, Message: "连接成功"}, nil
}

// postJSON 向 OpenAI 兼容服务发 POST JSON 请求并读取响应体；非 2xx 视为服务不可用。
func postJSON(ctx context.Context, url, apiKey string, body []byte, maxBytes int64) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAIUnavailable, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if key := strings.TrimSpace(apiKey); key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAIUnavailable, err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAIUnavailable, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: 状态码 %d", ErrAIUnavailable, resp.StatusCode)
	}
	return payload, nil
}

// chatCompletionsURL 补全为对话补全接口地址。
func chatCompletionsURL(apiURL string) string {
	base := strings.TrimRight(strings.TrimSpace(apiURL), "/")
	if base == "" {
		return base
	}
	return base + "/chat/completions"
}
