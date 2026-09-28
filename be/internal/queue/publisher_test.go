package queue

import (
	"context"
	"strings"
	"testing"

	"askbase/be/internal/config"

	amqp "github.com/rabbitmq/amqp091-go"
)

type stubPublishConfirmation struct {
	done <-chan struct{}
	ack  bool
}

func (c stubPublishConfirmation) Done() <-chan struct{} {
	return c.done
}

func (c stubPublishConfirmation) Acked() bool {
	return c.ack
}

func TestWaitForConfirmAcceptsAck(t *testing.T) {
	done := make(chan struct{})
	close(done)
	publisher := &Publisher{
		cfg:     config.QueueConfig{ConfirmTimeoutMs: 1000},
		returns: make(chan amqp.Return, 1),
	}

	if err := publisher.waitForConfirm(context.Background(), stubPublishConfirmation{done: done, ack: true}); err != nil {
		t.Fatalf("本次发布的 ACK 应成功: %v", err)
	}
}

func TestWaitForConfirmRejectsNack(t *testing.T) {
	done := make(chan struct{})
	close(done)
	publisher := &Publisher{
		cfg:     config.QueueConfig{ConfirmTimeoutMs: 1000},
		returns: make(chan amqp.Return, 1),
	}

	err := publisher.waitForConfirm(context.Background(), stubPublishConfirmation{done: done})
	if err == nil || !strings.Contains(err.Error(), "拒绝") {
		t.Fatalf("broker NACK 应使发布失败: %v", err)
	}
}

func TestWaitForConfirmRejectsReturnBeforeAck(t *testing.T) {
	done := make(chan struct{})
	close(done)
	returns := make(chan amqp.Return, 1)
	returns <- amqp.Return{ReplyText: "NO_ROUTE"}
	publisher := &Publisher{
		cfg:     config.QueueConfig{ConfirmTimeoutMs: 1000},
		returns: returns,
	}

	err := publisher.waitForConfirm(context.Background(), stubPublishConfirmation{done: done, ack: true})
	if err == nil || !strings.Contains(err.Error(), "NO_ROUTE") {
		t.Fatalf("无法路由时即使收到 ACK 也必须失败: %v", err)
	}
}
