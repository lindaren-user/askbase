package infra

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"askbase/be/internal/config"

	amqp "github.com/rabbitmq/amqp091-go"
)

// NewRabbitMQ 创建 RabbitMQ 连接。
func NewRabbitMQ(ctx context.Context, cfg config.RabbitMQConfig) (*amqp.Connection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dialer := &net.Dialer{}
	endpoint := "amqp://" + net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	conn, err := amqp.DialConfig(endpoint, amqp.Config{
		SASL: []amqp.Authentication{
			&amqp.PlainAuth{Username: cfg.Username, Password: cfg.Password},
		},
		Vhost: cfg.VHost,
		Dial: func(network, address string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, address)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("连接 RabbitMQ 失败: %w", err)
	}
	if err := ctx.Err(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}
