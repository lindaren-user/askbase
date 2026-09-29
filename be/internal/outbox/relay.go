// Package outbox 轮询 PostgreSQL 事务 Outbox 并可靠发布到消息代理。
package outbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	"askbase/be/internal/config"
	"askbase/be/internal/model"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type store interface {
	LockNextDue(ctx context.Context, scheduledStatus string, scheduledBefore time.Time, leasedStatus string, leaseBefore time.Time) (model.Outbox, bool, error)
	UpdateFields(ctx context.Context, id int64, conditions map[string]any, fields map[string]any) (bool, error)
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
	// 1. 初始化轮询和清理定时器。
	pollTicker := time.NewTicker(r.cfg.PollInterval())
	defer pollTicker.Stop()
	cleanupTicker := time.NewTicker(time.Hour)
	defer cleanupTicker.Stop()

	for {
		// 2. 发布到期事件；无事件时等待下一次轮询或清理。
		processed, publishErr, err := r.processNext(ctx)
		if err != nil {
			return err
		}
		if publishErr != nil {
			// 发布器连接可能已失效；退出后由进程管理器重建 RabbitMQ 连接。
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
			// 3. 定期清理已发布的历史记录。
			cutoff := time.Now().Add(-r.cfg.Retention())
			if err := r.store.DeletePublishedBefore(ctx, model.OutboxStatusPublished, cutoff); err != nil {
				zap.L().Warn("清理已发布 Outbox 失败", zap.Error(err))
			}
		}
	}
}

// processNext 领取一条到期事件，在事务外发布，再持久化发布结果。
// 返回值依次表示是否处理了事件、发布错误和存储错误。
func (r *Relay) processNext(ctx context.Context) (bool, error, error) {
	claimedAt := time.Now()
	// 每次领取都使用新令牌，避免过期领取者恢复后写回旧结果。
	leaseToken := uuid.NewString()
	var event model.Outbox
	var found bool
	if err := r.transactions.Within(ctx, func(ctx context.Context) error {
		var err error
		event, found, err = r.store.LockNextDue(
			ctx,
			model.OutboxStatusPending,
			claimedAt,
			model.OutboxStatusProcessing,
			claimedAt,
		)
		if err != nil || !found {
			return err
		}

		event.PublishAttempts++
		updated, err := r.store.UpdateFields(ctx, event.ID, nil, map[string]any{
			"status":           model.OutboxStatusProcessing,
			"publish_attempts": event.PublishAttempts,
			"lease_token":      leaseToken,
			"lease_until":      claimedAt.Add(r.cfg.LeaseDuration()),
			"updated_at":       claimedAt,
		})
		if err != nil {
			return err
		}
		if !updated {
			return fmt.Errorf("领取 Outbox 事件失败：记录不存在")
		}
		return nil
	}); err != nil {
		return false, nil, err
	}
	if !found {
		return false, nil, nil
	}

	// 领取事务已提交，RabbitMQ 网络调用期间不持有数据库行锁。
	if publishErr := r.publisher.PublishOutbox(ctx, event); publishErr != nil {
		finishedAt := time.Now()
		updateErr := r.finishClaim(ctx, event.ID, leaseToken, finishedAt, map[string]any{
			"status":          model.OutboxStatusPending, // 发布到 MQ 失败，重置消息为 pending
			"next_attempt_at": finishedAt.Add(publishBackoff(event.PublishAttempts, r.cfg.MaxBackoff())),
			"last_error":      truncateError(publishErr.Error()),
		})
		return true, publishErr, updateErr
	}

	finishedAt := time.Now()
	if err := r.finishClaim(ctx, event.ID, leaseToken, finishedAt, map[string]any{
		"status":       model.OutboxStatusPublished,
		"published_at": finishedAt,
		"last_error":   nil,
	}); err != nil {
		return true, nil, err
	}
	return true, nil, nil
}

// finishClaim 仅在本次领取仍有效时写入结果并清除租约。
func (r *Relay) finishClaim(ctx context.Context, id int64, leaseToken string, finishedAt time.Time, fields map[string]any) error {
	fields["lease_token"] = nil
	fields["lease_until"] = nil
	fields["updated_at"] = finishedAt

	return r.transactions.Within(ctx, func(ctx context.Context) error {
		// 旧领取者恢复时，令牌条件会阻止其覆盖新领取者的结果。
		updated, err := r.store.UpdateFields(ctx, id, map[string]any{
			"status":      model.OutboxStatusProcessing,
			"lease_token": leaseToken,
		}, fields)
		if err != nil {
			return err
		}
		if !updated {
			return fmt.Errorf("更新 Outbox 事件失败：领取租约已失效")
		}
		return nil
	})
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
