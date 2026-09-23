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
	channel  *amqp.Channel
	cfg      config.QueueConfig
	confirms <-chan amqp.Confirmation
	returns  <-chan amqp.Return
	mu       sync.Mutex
}

// NewPublisher 创建 publisher 专用 Channel 并启用 confirm。
func NewPublisher(conn *amqp.Connection, cfg config.QueueConfig) (*Publisher, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("创建 RabbitMQ publisher channel 失败: %w", err)
	}
	if err := DeclareTopology(ch, cfg); err != nil {
		_ = ch.Close()
		return nil, err
	}
	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		return nil, fmt.Errorf("启用 publisher confirm 失败: %w", err)
	}
	return &Publisher{
		channel:  ch,
		cfg:      cfg,
		confirms: ch.NotifyPublish(make(chan amqp.Confirmation, 1)),
		returns:  ch.NotifyReturn(make(chan amqp.Return, 1)),
	}, nil
}

// Close 关闭 publisher Channel。
func (p *Publisher) Close() error {
	return p.channel.Close()
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
	return p.publish(ctx, p.cfg.Exchange, p.cfg.RoutingKey, msg)
}

// PublishRetry 把消息发布到延迟重试队列。
func (p *Publisher) PublishRetry(ctx context.Context, msg Message) error {
	return p.publish(ctx, p.cfg.Exchange, p.cfg.RetryRoutingKey, msg)
}

func (p *Publisher) publish(ctx context.Context, exchange string, routingKey string, msg Message) error {
	body, err := marshalMessage(msg)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.channel.PublishWithContext(ctx, exchange, routingKey, true, false, amqp.Publishing{
		DeliveryMode: amqp.Persistent,
		ContentType:  "application/json",
		MessageId:    strconv.FormatInt(msg.EventID, 10),
		Timestamp:    time.Now(),
		Body:         body,
	}); err != nil {
		return fmt.Errorf("发布 RabbitMQ 消息失败: %w", err)
	}

	timer := time.NewTimer(p.cfg.ConfirmTimeout())
	defer timer.Stop()
	var returnReason string
	for {
		select {
		case ret, ok := <-p.returns:
			if !ok {
				return fmt.Errorf("RabbitMQ return channel 已关闭")
			}
			returnReason = ret.ReplyText
		case confirmation, ok := <-p.confirms:
			if !ok {
				return fmt.Errorf("RabbitMQ confirm channel 已关闭")
			}
			// RabbitMQ 会在 confirm 前发送 basic.return；两个通知 channel 同时就绪时再检查一次。
			select {
			case ret, ok := <-p.returns:
				if ok {
					returnReason = ret.ReplyText
				}
			default:
			}
			if returnReason != "" {
				return fmt.Errorf("RabbitMQ 消息不可路由: %s", returnReason)
			}
			if !confirmation.Ack {
				return fmt.Errorf("RabbitMQ 拒绝发布消息")
			}
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return fmt.Errorf("等待 RabbitMQ publisher confirm 超时")
		}
	}
}
