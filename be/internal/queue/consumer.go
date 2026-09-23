package queue

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"askbase/be/internal/config"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

// Consumer 使用手动 ACK 消费 RabbitMQ 文档任务。
type Consumer struct {
	channel   *amqp.Channel
	publisher retryPublisher
	cfg       config.QueueConfig
}

type retryPublisher interface {
	PublishRetry(ctx context.Context, msg Message) error
}

// NewConsumer 创建消费者专用 Channel。
func NewConsumer(conn *amqp.Connection, publisher retryPublisher, cfg config.QueueConfig) (*Consumer, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("创建 RabbitMQ consumer channel 失败: %w", err)
	}
	if err := DeclareTopology(ch, cfg); err != nil {
		_ = ch.Close()
		return nil, err
	}
	if err := ch.Qos(cfg.Prefetch, 0, false); err != nil {
		_ = ch.Close()
		return nil, fmt.Errorf("设置 RabbitMQ prefetch 失败: %w", err)
	}
	return &Consumer{channel: ch, publisher: publisher, cfg: cfg}, nil
}

// Close 关闭 consumer Channel。
func (c *Consumer) Close() error {
	return c.channel.Close()
}

// Run 启动固定并发消费者，直到上下文取消或 Channel 关闭。
func (c *Consumer) Run(ctx context.Context, handler Handler, terminal TerminalHandler) error {
	consumerTag := "askbase-" + uuid.NewString()
	deliveries, err := c.channel.Consume(c.cfg.Name, consumerTag, false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("启动 RabbitMQ 消费失败: %w", err)
	}
	closed := c.channel.NotifyClose(make(chan *amqp.Error, 1))
	errCh := make(chan error, c.cfg.Concurrency)
	var wg sync.WaitGroup
	for i := 0; i < c.cfg.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for delivery := range deliveries {
				if err := c.process(ctx, delivery, handler, terminal); err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
			}
		}()
	}

	var runErr error
	select {
	case <-ctx.Done():
		_ = c.channel.Cancel(consumerTag, false)
	case err := <-errCh:
		runErr = err
	case err := <-closed:
		if err != nil {
			runErr = fmt.Errorf("RabbitMQ consumer channel 关闭: %w", err)
		}
	}
	_ = c.channel.Close()
	wg.Wait()
	return runErr
}

func (c *Consumer) process(ctx context.Context, delivery amqp.Delivery, handler Handler, terminal TerminalHandler) error {
	msg, err := parseMessage(delivery.Body)
	if err != nil {
		zap.L().Error("RabbitMQ 消息损坏，转入 DLQ", zap.String("messageId", delivery.MessageId), zap.Error(err))
		return reject(delivery, false)
	}

	err = handler(ctx, msg)
	if err == nil {
		return ack(delivery)
	}
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return err
	}
	permanent, penalty := classify(err)
	if permanent || (penalty && msg.Attempt >= c.cfg.MaxDeliveries) {
		if terminal == nil {
			return fmt.Errorf("终态处理器未配置")
		}
		if terminalErr := terminal(ctx, msg, err); terminalErr != nil {
			if nackErr := delivery.Nack(false, true); nackErr != nil {
				return fmt.Errorf("终态落库失败且重新入队失败: %v: %w", terminalErr, nackErr)
			}
			return nil
		}
		zap.L().Warn("文档任务进入 DLQ",
			zap.Int64("documentId", msg.DocumentID),
			zap.Int("attempt", msg.Attempt),
			zap.Error(err),
		)
		return reject(delivery, false)
	}

	retry := msg
	if penalty {
		retry.Attempt++
	}
	if err := c.publisher.PublishRetry(ctx, retry); err != nil {
		_ = delivery.Nack(false, true)
		return fmt.Errorf("发布延迟重试失败: %w", err)
	}
	return ack(delivery)
}

func ack(delivery amqp.Delivery) error {
	if err := delivery.Ack(false); err != nil {
		return fmt.Errorf("RabbitMQ ACK 失败: %w", err)
	}
	return nil
}

func reject(delivery amqp.Delivery, requeue bool) error {
	if err := delivery.Reject(requeue); err != nil {
		return fmt.Errorf("RabbitMQ Reject 失败: %w", err)
	}
	return nil
}
