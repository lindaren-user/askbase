package queue

import (
	"context"
	"errors"
	"testing"

	"askbase/be/internal/config"

	amqp "github.com/rabbitmq/amqp091-go"
)

type fakeAcknowledger struct {
	acked    bool
	nacked   bool
	rejected bool
	requeue  bool
}

func (a *fakeAcknowledger) Ack(uint64, bool) error {
	a.acked = true
	return nil
}

func (a *fakeAcknowledger) Nack(_ uint64, _ bool, requeue bool) error {
	a.nacked = true
	a.requeue = requeue
	return nil
}

func (a *fakeAcknowledger) Reject(_ uint64, requeue bool) error {
	a.rejected = true
	a.requeue = requeue
	return nil
}

type fakeRetryPublisher struct {
	message Message
	err     error
}

func (p *fakeRetryPublisher) PublishRetry(_ context.Context, msg Message) error {
	p.message = msg
	return p.err
}

func testDelivery(t *testing.T, msg Message) (amqp.Delivery, *fakeAcknowledger) {
	t.Helper()
	body, err := marshalMessage(msg)
	if err != nil {
		t.Fatal(err)
	}
	acknowledger := &fakeAcknowledger{}
	return amqp.Delivery{Acknowledger: acknowledger, DeliveryTag: 1, Body: body}, acknowledger
}

func TestConsumerProcessSuccess(t *testing.T) {
	publisher := &fakeRetryPublisher{}
	consumer := &Consumer{publisher: publisher, cfg: config.QueueConfig{MaxDeliveries: 5}}
	delivery, acknowledger := testDelivery(t, Message{EventID: 1, DocumentID: 2, ParseVersion: 3, Attempt: 1})
	if err := consumer.process(context.Background(), delivery, func(context.Context, Message) error { return nil }, nil); err != nil {
		t.Fatal(err)
	}
	if !acknowledger.acked {
		t.Fatal("successful message must be acknowledged")
	}
}

func TestConsumerProcessSchedulesRetry(t *testing.T) {
	publisher := &fakeRetryPublisher{}
	consumer := &Consumer{publisher: publisher, cfg: config.QueueConfig{MaxDeliveries: 5}}
	delivery, acknowledger := testDelivery(t, Message{EventID: 1, DocumentID: 2, ParseVersion: 3, Attempt: 1})
	err := consumer.process(context.Background(), delivery, func(context.Context, Message) error {
		return errors.New("temporary")
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if publisher.message.Attempt != 2 || !acknowledger.acked {
		t.Fatalf("retry attempt=%d ack=%v", publisher.message.Attempt, acknowledger.acked)
	}
}

func TestConsumerProcessLockConflictKeepsAttempt(t *testing.T) {
	publisher := &fakeRetryPublisher{}
	consumer := &Consumer{publisher: publisher, cfg: config.QueueConfig{MaxDeliveries: 5}}
	delivery, acknowledger := testDelivery(t, Message{EventID: 1, DocumentID: 2, ParseVersion: 3, Attempt: 2})
	err := consumer.process(context.Background(), delivery, func(context.Context, Message) error {
		return RetryWithoutPenalty(errors.New("locked"))
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if publisher.message.Attempt != 2 || !acknowledger.acked {
		t.Fatalf("retry attempt=%d ack=%v", publisher.message.Attempt, acknowledger.acked)
	}
}

func TestConsumerProcessRetryPublishFailureRequeues(t *testing.T) {
	publisher := &fakeRetryPublisher{err: errors.New("broker unavailable")}
	consumer := &Consumer{publisher: publisher, cfg: config.QueueConfig{MaxDeliveries: 5}}
	delivery, acknowledger := testDelivery(t, Message{EventID: 1, DocumentID: 2, ParseVersion: 3, Attempt: 1})
	err := consumer.process(context.Background(), delivery, func(context.Context, Message) error {
		return errors.New("temporary")
	}, nil)
	if err == nil {
		t.Fatal("retry publish failure must stop the consumer")
	}
	if !acknowledger.nacked || !acknowledger.requeue || acknowledger.acked {
		t.Fatalf("nack=%v requeue=%v ack=%v", acknowledger.nacked, acknowledger.requeue, acknowledger.acked)
	}
}

func TestConsumerProcessExhaustedGoesToDLQ(t *testing.T) {
	consumer := &Consumer{publisher: &fakeRetryPublisher{}, cfg: config.QueueConfig{MaxDeliveries: 2}}
	delivery, acknowledger := testDelivery(t, Message{EventID: 1, DocumentID: 2, ParseVersion: 3, Attempt: 2})
	terminalCalled := false
	err := consumer.process(context.Background(), delivery, func(context.Context, Message) error {
		return errors.New("still failing")
	}, func(context.Context, Message, error) error {
		terminalCalled = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !terminalCalled || !acknowledger.rejected || acknowledger.requeue {
		t.Fatalf("terminal=%v rejected=%v requeue=%v", terminalCalled, acknowledger.rejected, acknowledger.requeue)
	}
}

func TestConsumerProcessTerminalFailureRequeues(t *testing.T) {
	consumer := &Consumer{publisher: &fakeRetryPublisher{}, cfg: config.QueueConfig{MaxDeliveries: 1}}
	delivery, acknowledger := testDelivery(t, Message{EventID: 1, DocumentID: 2, ParseVersion: 3, Attempt: 1})
	err := consumer.process(context.Background(), delivery, func(context.Context, Message) error {
		return Permanent(errors.New("bad file"))
	}, func(context.Context, Message, error) error {
		return errors.New("database unavailable")
	})
	if err != nil {
		t.Fatal(err)
	}
	if !acknowledger.nacked || !acknowledger.requeue {
		t.Fatal("terminal persistence failure must requeue")
	}
}

func TestConsumerProcessMalformedGoesToDLQ(t *testing.T) {
	acknowledger := &fakeAcknowledger{}
	delivery := amqp.Delivery{Acknowledger: acknowledger, DeliveryTag: 1, Body: []byte("{")}
	consumer := &Consumer{publisher: &fakeRetryPublisher{}, cfg: config.QueueConfig{MaxDeliveries: 5}}
	if err := consumer.process(context.Background(), delivery, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !acknowledger.rejected || acknowledger.requeue {
		t.Fatal("malformed message must be rejected to DLQ")
	}
}
