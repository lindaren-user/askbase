package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"askbase/be/internal/model"
)

// Message 一条文档解析任务。
type Message = model.DocumentParseEvent

// Handler 处理文档解析任务。
type Handler func(ctx context.Context, msg Message) error

// TerminalHandler 在永久失败或重试耗尽时可靠记录业务终态。
type TerminalHandler func(ctx context.Context, msg Message, cause error) error

type classifiedError struct {
	err       error
	permanent bool
	penalty   bool
}

func (e classifiedError) Error() string { return e.err.Error() }
func (e classifiedError) Unwrap() error { return e.err }

// Permanent 把错误标记为不可重试；终态落库后消息进入 DLQ。
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return classifiedError{err: err, permanent: true, penalty: true}
}

// RetryWithoutPenalty 请求延迟重试但不增加业务尝试次数。
func RetryWithoutPenalty(err error) error {
	if err == nil {
		return nil
	}
	return classifiedError{err: err, penalty: false}
}

func classify(err error) (permanent bool, penalty bool) {
	penalty = true
	var classified classifiedError
	if errors.As(err, &classified) {
		return classified.permanent, classified.penalty
	}
	return false, true
}

func parseMessage(body []byte) (Message, error) {
	var msg Message
	if err := json.Unmarshal(body, &msg); err != nil {
		return Message{}, fmt.Errorf("解析消息 JSON 失败: %w", err)
	}
	if msg.EventID <= 0 || msg.DocumentID <= 0 || msg.ParseVersion <= 0 || msg.Attempt <= 0 {
		return Message{}, fmt.Errorf("消息字段非法")
	}
	return msg, nil
}

func marshalMessage(msg Message) ([]byte, error) {
	body, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("编码文档解析消息失败: %w", err)
	}
	return body, nil
}
