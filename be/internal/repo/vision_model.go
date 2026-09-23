package repo

import (
	"context"
	"errors"
	"fmt"

	"askbase/be/internal/model"

	"gorm.io/gorm"
)

// ErrVisionModelNotFound 视觉模型不存在或不属于当前用户。
var ErrVisionModelNotFound = errors.New("vision model not found")

// VisionModelRepository 用户视觉模型数据访问。
type VisionModelRepository struct {
	db *gorm.DB
}

// NewVisionModelRepository 创建仓储。
func NewVisionModelRepository(db *gorm.DB) *VisionModelRepository {
	return &VisionModelRepository{db: db}
}

// Create 新增视觉模型。
func (r *VisionModelRepository) Create(ctx context.Context, m model.VisionModel) (model.VisionModel, error) {
	if err := conn(ctx, r.db).Create(&m).Error; err != nil {
		return model.VisionModel{}, fmt.Errorf("创建视觉模型失败: %w", err)
	}
	return m, nil
}

// GetByUser 按用户与 ID 查询。
func (r *VisionModelRepository) GetByUser(ctx context.Context, userID int64, id int64) (model.VisionModel, error) {
	var m model.VisionModel
	err := conn(ctx, r.db).Where("id = ? AND user_id = ?", id, userID).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.VisionModel{}, ErrVisionModelNotFound
	}
	if err != nil {
		return model.VisionModel{}, fmt.Errorf("查询视觉模型失败: %w", err)
	}
	return m, nil
}

// GetByUserAndName 按用户与展示名查询。
func (r *VisionModelRepository) GetByUserAndName(ctx context.Context, userID int64, name string) (model.VisionModel, error) {
	var m model.VisionModel
	err := conn(ctx, r.db).Where("user_id = ? AND name = ?", userID, name).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.VisionModel{}, ErrVisionModelNotFound
	}
	if err != nil {
		return model.VisionModel{}, fmt.Errorf("查询视觉模型失败: %w", err)
	}
	return m, nil
}

// GetByID 按 ID 查询（流水线内部用）。
func (r *VisionModelRepository) GetByID(ctx context.Context, id int64) (model.VisionModel, error) {
	var m model.VisionModel
	err := conn(ctx, r.db).Where("id = ?", id).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.VisionModel{}, ErrVisionModelNotFound
	}
	if err != nil {
		return model.VisionModel{}, fmt.Errorf("查询视觉模型失败: %w", err)
	}
	return m, nil
}

// ListByUser 列出用户视觉模型。
func (r *VisionModelRepository) ListByUser(ctx context.Context, userID int64) ([]model.VisionModel, error) {
	var items []model.VisionModel
	if err := conn(ctx, r.db).
		Where("user_id = ?", userID).
		Order("id ASC").
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("查询视觉模型列表失败: %w", err)
	}
	return orEmpty(items), nil
}

// Update 更新视觉模型字段。
func (r *VisionModelRepository) Update(ctx context.Context, userID int64, id int64, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}
	res := conn(ctx, r.db).
		Model(&model.VisionModel{}).
		Where("id = ? AND user_id = ?", id, userID).
		Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("更新视觉模型失败: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrVisionModelNotFound
	}
	return nil
}

// DeleteByUser 删除一条视觉模型。
func (r *VisionModelRepository) DeleteByUser(ctx context.Context, userID int64, id int64) error {
	res := conn(ctx, r.db).Where("id = ? AND user_id = ?", id, userID).Delete(&model.VisionModel{})
	if res.Error != nil {
		return fmt.Errorf("删除视觉模型失败: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrVisionModelNotFound
	}
	return nil
}

// DeleteAllByUser 注销账号时清空该用户视觉模型。
func (r *VisionModelRepository) DeleteAllByUser(ctx context.Context, userID int64) error {
	if err := conn(ctx, r.db).Where("user_id = ?", userID).Delete(&model.VisionModel{}).Error; err != nil {
		return fmt.Errorf("删除用户视觉模型失败: %w", err)
	}
	return nil
}
