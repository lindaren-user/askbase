package mail

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type resendSender struct {
	apiKey string
	from   string
	client *http.Client
}

type resendRequest struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Subject string `json:"subject"`
	Text    string `json:"text"`
	HTML    string `json:"html"`
}

// NewResendSender 创建通过 Resend HTTP API 发信的实现。
func NewResendSender(apiKey string, from string) Sender {
	return &resendSender{
		apiKey: apiKey,
		from:   strings.TrimSpace(from),
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (s *resendSender) Send(to string, subject string, textBody string, htmlBody string) error {
	payload := resendRequest{
		From:    s.from,
		To:      to,
		Subject: subject,
		Text:    textBody,
		HTML:    htmlBody,
	}

	buf, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化 Resend 请求失败: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("创建 Resend 请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("Resend 请求发送失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		message := strings.TrimSpace(string(body))
		if message == "" {
			return fmt.Errorf("Resend 返回错误状态码: %d", resp.StatusCode)
		}
		return fmt.Errorf("Resend 返回错误状态码: %d, 响应: %s", resp.StatusCode, message)
	}

	return nil
}
