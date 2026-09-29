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

// LockNextDue 在当前事务中锁定一条符合调用方状态和时间条件的记录。
func (r *OutboxRepository) LockNextDue(
	ctx context.Context,
	scheduledStatus string,
	scheduledBefore time.Time,
	leasedStatus string,
	leaseBefore time.Time,
) (model.Outbox, bool, error) {
	var event model.Outbox
	// 对应 SQL：
	// SELECT * FROM t_outbox
	// WHERE (status = ? AND next_attempt_at <= ?) OR (status = ? AND lease_until <= ?)
	// ORDER BY id ASC LIMIT 1 FOR UPDATE SKIP LOCKED;
	err := conn(ctx, r.db).Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("(status = ? AND next_attempt_at <= ?) OR (status = ? AND lease_until <= ?)",
			scheduledStatus, scheduledBefore, leasedStatus, leaseBefore).
		Order("id ASC").
		Take(&event).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Outbox{}, false, nil
	}
	if err != nil {
		return model.Outbox{}, false, fmt.Errorf("锁定 Outbox 记录失败: %w", err)
	}
	return event, true, nil
}

// UpdateFields 按调用方指定的等值条件原子更新列，返回是否匹配到记录。
func (r *OutboxRepository) UpdateFields(ctx context.Context, id int64, conditions map[string]any, fields map[string]any) (bool, error) {
	query := conn(ctx, r.db).Model(&model.Outbox{}).Where("id = ?", id)
	if len(conditions) > 0 {
		query = query.Where(conditions)
	}
	result := query.Updates(fields)
	if result.Error != nil {
		return false, fmt.Errorf("更新 Outbox 记录失败: %w", result.Error)
	}
	return result.RowsAffected == 1, nil
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
