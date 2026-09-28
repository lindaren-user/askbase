package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"askbase/be/internal/config"
	"askbase/be/internal/model"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Publisher 使用 publisher confirm 可靠发布 Outbox 与重试消息。
type Publisher struct {
	conn    *amqp.Connection
	channel *amqp.Channel
	cfg     config.QueueConfig
	returns <-chan amqp.Return
	mu      sync.Mutex
	closed  bool
}

type publishConfirmation interface {
	Done() <-chan struct{}
	Acked() bool
}

// NewPublisher 创建 confirm 发布器；确认异常后可在同一连接上重建 Channel。
func NewPublisher(conn *amqp.Connection, cfg config.QueueConfig) (*Publisher, error) {
	publisher := &Publisher{conn: conn, cfg: cfg}
	if err := publisher.openChannel(); err != nil {
		return nil, err
	}
	return publisher, nil
}

// openChannel 创建新的 confirm Channel；调用方需持有 mu 或尚未暴露 Publisher。
func (p *Publisher) openChannel() error {
	ch, err := p.conn.Channel()
	if err != nil {
		return fmt.Errorf("创建 RabbitMQ publisher channel 失败: %w", err)
	}
	if err := DeclareTopology(ch, p.cfg); err != nil {
		_ = ch.Close()
		return err
	}
	// 必须启用 confirm 模式，否则发布方法不会返回确认对象。
	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		return fmt.Errorf("启用 publisher confirm 失败: %w", err)
	}
	p.channel = ch
	p.returns = ch.NotifyReturn(make(chan amqp.Return, 1))
	return nil
}

// closeChannel 清空通知通道并关闭当前 Channel；调用方需持有 mu。
func (p *Publisher) closeChannel() error {
	if p.channel == nil {
		return nil
	}
	channel := p.channel
	p.channel = nil
	p.returns = nil
	return channel.Close()
}

// Close 关闭 publisher Channel。
func (p *Publisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return p.closeChannel()
}

// PublishOutbox 发布一条 Outbox 文档解析事件。
func (p *Publisher) PublishOutbox(ctx context.Context, event model.Outbox) error {
	if event.Topic != model.OutboxTopicDocumentParse {
		return fmt.Errorf("不支持的 Outbox topic: %s", event.Topic)
	}
	var msg Message
	if err := json.Unmarshal(event.Payload, &msg); err != nil {
		return fmt.Errorf("解析 Outbox payload 失败: %w", err)
	}
	msg.EventID = event.ID
	if msg.Attempt <= 0 {
		msg.Attempt = 1
	}
	return p.publish(ctx, p.cfg.RoutingKey, msg)
}

// PublishRetry 把消息发布到延迟重试队列。
func (p *Publisher) PublishRetry(ctx context.Context, msg Message) error {
	return p.publish(ctx, p.cfg.RetryRoutingKey, msg)
}

func (p *Publisher) publish(ctx context.Context, routingKey string, msg Message) error {
	body, err := marshalMessage(msg)
	if err != nil {
		return err
	}
	// 当前按低吞吐量场景设计：串行发布并逐条等待 broker 的 publisher confirm。
	// TODO: 吞吐量成为瓶颈时，改用批量等待或异步 confirm，并逐条关联 ACK/NACK 与 mandatory return。
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return fmt.Errorf("RabbitMQ publisher 已关闭")
	}
	if p.channel == nil {
		if err := p.openChannel(); err != nil {
			return err
		}
	}

	// 返回的确认对象只对应本次发布；调用成功不代表 broker 已确认。
	// ctx 只在开始发布前检查，开始后取消不会中断底层写入；后续需单独等待 ACK/NACK。
	// https://pkg.go.dev/github.com/rabbitmq/amqp091-go#Channel.PublishWithDeferredConfirmWithContext
	confirmation, err := p.channel.PublishWithDeferredConfirmWithContext(ctx, p.cfg.Exchange, routingKey, true, false, amqp.Publishing{
		DeliveryMode: amqp.Persistent,
		ContentType:  "application/json",
		MessageId:    strconv.FormatInt(msg.EventID, 10),
		Timestamp:    time.Now(),
		Body:         body,
	})
	if err != nil {
		// 发送报错也无法判定 broker 是否收到，下一次必须使用新 Channel。
		_ = p.closeChannel()
		return fmt.Errorf("发布 RabbitMQ 消息失败: %w", err)
	}
	if confirmation == nil {
		_ = p.closeChannel()
		return fmt.Errorf("RabbitMQ publisher confirm 未启用")
	}
	if err := p.waitForConfirm(ctx, confirmation); err != nil {
		// 超时或其他异常后，旧 Channel 可能继续收到 confirm 和 return。
		_ = p.closeChannel()
		return err
	}
	return nil
}

// waitForConfirm 等待本次发布的确认，同时检查 mandatory 消息是否被退回。
func (p *Publisher) waitForConfirm(ctx context.Context, confirmation publishConfirmation) error {
	timer := time.NewTimer(p.cfg.ConfirmTimeout())
	defer timer.Stop()
	select {
	case ret, ok := <-p.returns:
		if !ok {
			return fmt.Errorf("RabbitMQ return channel 已关闭")
		}
		return fmt.Errorf("RabbitMQ 消息不可路由: %s", ret.ReplyText)
	case <-confirmation.Done():
		// Done 表示本次发布收到 confirm，Acked 才区分 ACK/NACK。
		// RabbitMQ 在 confirm 前发送 basic.return；确认就绪后检查已缓存的退回消息。
		// https://www.rabbitmq.com/docs/confirms
		select {
		case ret, ok := <-p.returns:
			if !ok {
				return fmt.Errorf("RabbitMQ return channel 已关闭")
			}
			return fmt.Errorf("RabbitMQ 消息不可路由: %s", ret.ReplyText)
		default:
		}
		if !confirmation.Acked() {
			return fmt.Errorf("RabbitMQ 拒绝发布消息")
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return fmt.Errorf("等待 RabbitMQ publisher confirm 超时")
	}
}
