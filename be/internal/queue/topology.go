package queue

import (
	"fmt"

	"askbase/be/internal/config"

	amqp "github.com/rabbitmq/amqp091-go"
)

// DeclareTopology 声明文档任务所需的交换机、主队列、重试队列与 DLQ。
func DeclareTopology(ch *amqp.Channel, cfg config.QueueConfig) error {
	if err := ch.ExchangeDeclare(cfg.Exchange, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("声明任务交换机失败: %w", err)
	}
	if err := ch.ExchangeDeclare(cfg.DeadExchange, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("声明死信交换机失败: %w", err)
	}

	mainArgs := amqp.Table{
		"x-queue-type":              "quorum",  // 多节点复制，提高消息持久性与可用性。
		"x-delivery-limit":          int64(-1), // 关闭 Broker 次数限制，由应用控制重试次数。
		"x-dead-letter-exchange":    cfg.DeadExchange,
		"x-dead-letter-routing-key": cfg.DeadRoutingKey,
	}
	if _, err := ch.QueueDeclare(cfg.Name, true, false, false, false, mainArgs); err != nil {
		return fmt.Errorf("声明主队列失败: %w", err)
	}
	if err := ch.QueueBind(cfg.Name, cfg.RoutingKey, cfg.Exchange, false, nil); err != nil {
		return fmt.Errorf("绑定主队列失败: %w", err)
	}

	retryArgs := amqp.Table{
		"x-queue-type":              "quorum",
		"x-message-ttl":             cfg.RetryDelayMs, // TTL 到期后通过 DLX 返回主队列，无需延时插件。
		"x-dead-letter-exchange":    cfg.Exchange,
		"x-dead-letter-routing-key": cfg.RoutingKey,
	}
	if _, err := ch.QueueDeclare(cfg.RetryQueue, true, false, false, false, retryArgs); err != nil {
		return fmt.Errorf("声明重试队列失败: %w", err)
	}
	if err := ch.QueueBind(cfg.RetryQueue, cfg.RetryRoutingKey, cfg.Exchange, false, nil); err != nil {
		return fmt.Errorf("绑定重试队列失败: %w", err)
	}

	deadArgs := amqp.Table{
		"x-queue-type":  "quorum",
		"x-message-ttl": cfg.DeadRetentionMs, // DLQ 消息超过保留期后自动清理。
	}
	if _, err := ch.QueueDeclare(cfg.DeadQueue, true, false, false, false, deadArgs); err != nil {
		return fmt.Errorf("声明死信队列失败: %w", err)
	}
	if err := ch.QueueBind(cfg.DeadQueue, cfg.DeadRoutingKey, cfg.DeadExchange, false, nil); err != nil {
		return fmt.Errorf("绑定死信队列失败: %w", err)
	}
	return nil
}
