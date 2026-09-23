package repo

import (
	"context"
	"errors"
	"fmt"

	"askbase/be/internal/model"

	"gorm.io/gorm"
)

// ErrEmbedModelNotFound 嵌入模型不存在或不属于当前用户。
var ErrEmbedModelNotFound = errors.New("embed model not found")

// EmbedModelRepository 用户嵌入模型数据访问。
type EmbedModelRepository struct {
	db *gorm.DB
}

// NewEmbedModelRepository 创建仓储。
func NewEmbedModelRepository(db *gorm.DB) *EmbedModelRepository {
	return &EmbedModelRepository{db: db}
}

// Create 新增嵌入模型。
func (r *EmbedModelRepository) Create(ctx context.Context, m model.EmbedModel) (model.EmbedModel, error) {
	if err := conn(ctx, r.db).Create(&m).Error; err != nil {
		return model.EmbedModel{}, fmt.Errorf("创建嵌入模型失败: %w", err)
	}
	return m, nil
}

// GetByUser 按用户与 ID 查询。
func (r *EmbedModelRepository) GetByUser(ctx context.Context, userID int64, id int64) (model.EmbedModel, error) {
	var m model.EmbedModel
	err := conn(ctx, r.db).Where("id = ? AND user_id = ?", id, userID).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.EmbedModel{}, ErrEmbedModelNotFound
	}
	if err != nil {
		return model.EmbedModel{}, fmt.Errorf("查询嵌入模型失败: %w", err)
	}
	return m, nil
}

// GetByID 按 ID 查询（流水线内部用）。
func (r *EmbedModelRepository) GetByID(ctx context.Context, id int64) (model.EmbedModel, error) {
	var m model.EmbedModel
	err := conn(ctx, r.db).Where("id = ?", id).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.EmbedModel{}, ErrEmbedModelNotFound
	}
	if err != nil {
		return model.EmbedModel{}, fmt.Errorf("查询嵌入模型失败: %w", err)
	}
	return m, nil
}

// ListByUser 列出用户嵌入模型。
func (r *EmbedModelRepository) ListByUser(ctx context.Context, userID int64) ([]model.EmbedModel, error) {
	var items []model.EmbedModel
	if err := conn(ctx, r.db).
		Where("user_id = ?", userID).
		Order("id ASC").
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("查询嵌入模型列表失败: %w", err)
	}
	return orEmpty(items), nil
}

// Update 更新嵌入模型字段。
func (r *EmbedModelRepository) Update(ctx context.Context, userID int64, id int64, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}
	res := conn(ctx, r.db).
		Model(&model.EmbedModel{}).
		Where("id = ? AND user_id = ?", id, userID).
		Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("更新嵌入模型失败: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrEmbedModelNotFound
	}
	return nil
}

// DeleteByUser 删除一条嵌入模型。
func (r *EmbedModelRepository) DeleteByUser(ctx context.Context, userID int64, id int64) error {
	res := conn(ctx, r.db).Where("id = ? AND user_id = ?", id, userID).Delete(&model.EmbedModel{})
	if res.Error != nil {
		return fmt.Errorf("删除嵌入模型失败: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrEmbedModelNotFound
	}
	return nil
}

// DeleteAllByUser 注销账号时清空该用户嵌入模型。
func (r *EmbedModelRepository) DeleteAllByUser(ctx context.Context, userID int64) error {
	if err := conn(ctx, r.db).Where("user_id = ?", userID).Delete(&model.EmbedModel{}).Error; err != nil {
		return fmt.Errorf("删除用户嵌入模型失败: %w", err)
	}
	return nil
}
