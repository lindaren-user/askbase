package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"askbase/be/internal/middleware"
	"askbase/be/internal/model"
	"askbase/be/internal/service"
	"askbase/be/pkg/resp"

	"go.uber.org/zap"
)

// HandleChatCompletions SSE 流式对话：event 为 meta / delta / done / error。
func (h *Handler) HandleChatCompletions(w http.ResponseWriter, req *http.Request) {
	user := middleware.CurrentUser(req)
	var body model.ChatCompletionRequest
	if !resp.Decode(w, req, &body, "请求格式错误") {
		return
	}
	events, err := h.services.Chat.ChatStream(req.Context(), user.ID, body)
	if h.writeServiceErr(w, err, "会话不存在", "对话参数错误", "对话失败") {
		return
	}

	_, ok := w.(http.Flusher)
	if !ok {
		resp.Fail(w, http.StatusInternalServerError, "响应不支持流式输出", nil)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	for event := range events {
		if err := writeChatEvent(w, event); err != nil {
			fields := []zap.Field{
				zap.Int64("userId", user.ID),
				zap.Int64("sessionId", body.SessionID),
				zap.String("eventType", event.Type),
				zap.Error(err),
			}
			if req.Context().Err() != nil {
				zap.L().Info("聊天流连接已断开", fields...)
			} else {
				zap.L().Warn("写入聊天流失败", fields...)
			}
			return
		}
	}
}

// writeChatEvent 写入并立即刷新一帧 SSE。
func writeChatEvent(w http.ResponseWriter, event service.ChatEvent) error {
	var name string
	var payload any
	switch event.Type {
	case "meta":
		name = "meta"
		payload = map[string]any{
			"sessionId":          event.SessionID,
			"userMessageId":      event.UserMessageID,
			"assistantMessageId": event.AssistantMessageID,
		}
	case "delta":
		name = "delta"
		payload = map[string]any{"content": event.Content}
	case "done":
		name = "done"
		payload = map[string]any{
			"finishReason": event.FinishReason,
			"citations":    event.Citations,
			"content":      event.Content,
		}
	default:
		name = "error"
		msg := "生成回复失败"
		if event.Err != nil {
			msg = safeChatError(event.Err)
		}
		payload = map[string]any{"message": msg}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化聊天事件失败: %w", err)
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data); err != nil {
		return fmt.Errorf("写入聊天事件失败: %w", err)
	}
	if err := http.NewResponseController(w).Flush(); err != nil {
		return fmt.Errorf("刷新聊天事件失败: %w", err)
	}
	return nil
}

// safeChatError 内部错误映射为安全文案，不暴露细节。
func safeChatError(err error) string {
	return "生成回复失败，请稍后重试"
}
