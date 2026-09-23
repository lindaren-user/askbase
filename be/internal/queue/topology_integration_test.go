package queue

import (
	"os"
	"testing"

	"askbase/be/internal/config"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

func TestDeclareTopologyIntegration(t *testing.T) {
	url := os.Getenv("RABBITMQ_TEST_URL")
	if url == "" {
		t.Skip("RABBITMQ_TEST_URL is not set")
	}
	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()

	suffix := uuid.NewString()
	cfg := config.QueueConfig{
		Exchange:        "askbase.test.documents." + suffix,
		Name:            "askbase.test.document.parse." + suffix,
		RoutingKey:      "document.parse",
		RetryQueue:      "askbase.test.document.parse.retry." + suffix,
		RetryRoutingKey: "document.parse.retry",
		DeadExchange:    "askbase.test.documents.dead." + suffix,
		DeadQueue:       "askbase.test.document.parse.dead." + suffix,
		DeadRoutingKey:  "document.parse.dead",
		RetryDelayMs:    1000,
		DeadRetentionMs: 1000,
	}
	defer func() {
		_, _ = ch.QueueDelete(cfg.Name, false, false, false)
		_, _ = ch.QueueDelete(cfg.RetryQueue, false, false, false)
		_, _ = ch.QueueDelete(cfg.DeadQueue, false, false, false)
		_ = ch.ExchangeDelete(cfg.Exchange, false, false)
		_ = ch.ExchangeDelete(cfg.DeadExchange, false, false)
	}()

	if err := DeclareTopology(ch, cfg); err != nil {
		t.Fatal(err)
	}
}
