// Package outbox 轮询 PostgreSQL 事务 Outbox 并可靠发布到消息代理。
package outbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	"askbase/be/internal/config"
	"askbase/be/internal/model"

	"go.uber.org/zap"
)

type store interface {
	LockNextByStatus(ctx context.Context, status string, dueAt time.Time) (model.Outbox, bool, error)
	UpdatePublishFailure(ctx context.Context, id int64, attempts int, nextAttemptAt time.Time, lastError string, updatedAt time.Time) error
	UpdatePublished(ctx context.Context, id int64, status string, publishedAt time.Time) error
	DeletePublishedBefore(ctx context.Context, status string, cutoff time.Time) error
}

type transactionManager interface {
	Within(ctx context.Context, fn func(ctx context.Context) error) error
}

type publisher interface {
	PublishOutbox(ctx context.Context, event model.Outbox) error
}

// Relay 把已提交的 Outbox 事件转发到 RabbitMQ。
type Relay struct {
	transactions transactionManager
	store        store
	publisher    publisher
	cfg          config.OutboxConfig
}

// NewRelay 创建 Outbox relay。
func NewRelay(transactions transactionManager, store store, publisher publisher, cfg config.OutboxConfig) *Relay {
	return &Relay{transactions: transactions, store: store, publisher: publisher, cfg: cfg}
}

// Run 持续发布到期事件并定期清理已发布记录。
func (r *Relay) Run(ctx context.Context) error {
	pollTicker := time.NewTicker(r.cfg.PollInterval())
	defer pollTicker.Stop()
	cleanupTicker := time.NewTicker(time.Hour)
	defer cleanupTicker.Stop()

	for {
		processed, publishErr, err := r.processNext(ctx)
		if err != nil {
			return err
		}
		if publishErr != nil {
			return fmt.Errorf("发布 Outbox 事件失败: %w", publishErr)
		}
		if processed {
			continue
		}

		select {
		case <-ctx.Done():
			return nil
		case <-pollTicker.C:
		case <-cleanupTicker.C:
			cutoff := time.Now().Add(-r.cfg.Retention())
			if err := r.store.DeletePublishedBefore(ctx, model.OutboxStatusPublished, cutoff); err != nil {
				zap.L().Warn("清理已发布 Outbox 失败", zap.Error(err))
			}
		}
	}
}

func (r *Relay) processNext(ctx context.Context) (bool, error, error) {
	processed := false
	var publishErr error
	err := r.transactions.Within(ctx, func(ctx context.Context) error {
		event, found, err := r.store.LockNextByStatus(ctx, model.OutboxStatusPending, time.Now())
		if err != nil || !found {
			return err
		}
		processed = true
		if err := r.publisher.PublishOutbox(ctx, event); err != nil {
			publishErr = err
			now := time.Now()
			return r.store.UpdatePublishFailure(
				ctx,
				event.ID,
				event.PublishAttempts+1,
				now.Add(publishBackoff(event.PublishAttempts+1, r.cfg.MaxBackoff())),
				truncateError(err.Error()),
				now,
			)
		}
		return r.store.UpdatePublished(ctx, event.ID, model.OutboxStatusPublished, time.Now())
	})
	return processed, publishErr, err
}

func publishBackoff(attempt int, maximum time.Duration) time.Duration {
	if maximum <= 0 {
		maximum = time.Minute
	}
	delay := time.Second
	for i := 1; i < attempt && delay < maximum; i++ {
		delay *= 2
	}
	if delay > maximum {
		return maximum
	}
	return delay
}

func truncateError(message string) string {
	const maxLength = 4096
	message = strings.TrimSpace(message)
	if len(message) <= maxLength {
		return message
	}
	return message[:maxLength]
}
