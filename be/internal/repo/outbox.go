package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"askbase/be/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// OutboxRepository 访问事务 Outbox。
type OutboxRepository struct {
	db *gorm.DB
}

// NewOutboxRepository 创建事务 Outbox 仓储。
func NewOutboxRepository(db *gorm.DB) *OutboxRepository {
	return &OutboxRepository{db: db}
}

// Create 在当前事务中写入待发布事件。
func (r *OutboxRepository) Create(ctx context.Context, event model.Outbox) (model.Outbox, error) {
	if err := conn(ctx, r.db).Create(&event).Error; err != nil {
		return model.Outbox{}, fmt.Errorf("创建 Outbox 事件失败: %w", err)
	}
	return event, nil
}

// LockNextByStatus 锁定一条指定状态且已到期的事件；调用方负责开启事务。
func (r *OutboxRepository) LockNextByStatus(ctx context.Context, status string, dueAt time.Time) (model.Outbox, bool, error) {
	var event model.Outbox
	err := conn(ctx, r.db).Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("status = ? AND next_attempt_at <= ?", status, dueAt).
		Order("id ASC").
		Take(&event).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Outbox{}, false, nil
	}
	if err != nil {
		return model.Outbox{}, false, fmt.Errorf("锁定 Outbox 事件失败: %w", err)
	}
	return event, true, nil
}

// UpdatePublishFailure 持久化调用方计算好的发布失败结果。
func (r *OutboxRepository) UpdatePublishFailure(ctx context.Context, id int64, attempts int, nextAttemptAt time.Time, lastError string, updatedAt time.Time) error {
	if err := conn(ctx, r.db).Model(&model.Outbox{}).Where("id = ?", id).Updates(map[string]any{
		"publish_attempts": attempts,
		"next_attempt_at":  nextAttemptAt,
		"last_error":       lastError,
		"updated_at":       updatedAt,
	}).Error; err != nil {
		return fmt.Errorf("更新 Outbox 发布失败结果: %w", err)
	}
	return nil
}

// UpdatePublished 持久化调用方给出的发布成功状态与时间。
func (r *OutboxRepository) UpdatePublished(ctx context.Context, id int64, status string, publishedAt time.Time) error {
	if err := conn(ctx, r.db).Model(&model.Outbox{}).Where("id = ?", id).Updates(map[string]any{
		"status":       status,
		"published_at": publishedAt,
		"last_error":   nil,
		"updated_at":   publishedAt,
	}).Error; err != nil {
		return fmt.Errorf("更新 Outbox 发布成功结果: %w", err)
	}
	return nil
}

// DeletePublishedBefore 删除指定状态且超过保留期的事件。
func (r *OutboxRepository) DeletePublishedBefore(ctx context.Context, status string, cutoff time.Time) error {
	if err := conn(ctx, r.db).
		Where("status = ? AND published_at < ?", status, cutoff).
		Delete(&model.Outbox{}).Error; err != nil {
		return fmt.Errorf("清理已发布 Outbox 失败: %w", err)
	}
	return nil
}
