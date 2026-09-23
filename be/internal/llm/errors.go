package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

func providerErrorMessage(status int, body []byte) string {
	msg := extractJSONErrorMessage(body)
	if msg == "" {
		return fmt.Sprintf("视觉服务返回 %d", status)
	}
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "user location is not supported") {
		return "当前地区无法使用该视觉模型，请更换模型或接口"
	}
	return msg
}

func extractJSONErrorMessage(body []byte) string {
	var node any
	if json.Unmarshal(body, &node) != nil {
		return ""
	}
	return firstErrorMessage(node)
}

func firstErrorMessage(node any) string {
	switch value := node.(type) {
	case map[string]any:
		if raw, ok := value["metadata"].(map[string]any); ok {
			if text, ok := raw["raw"].(string); ok {
				if inner := extractJSONErrorMessage([]byte(text)); inner != "" {
					return inner
				}
			}
		}
		if raw, ok := value["error"]; ok {
			if inner := firstErrorMessage(raw); inner != "" {
				return inner
			}
		}
		if message, ok := value["message"].(string); ok {
			message = strings.TrimSpace(message)
			if message != "" && message != "Provider returned error" {
				return message
			}
		}
	case string:
		if inner := extractJSONErrorMessage([]byte(value)); inner != "" {
			return inner
		}
	}
	return ""
}

func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}
