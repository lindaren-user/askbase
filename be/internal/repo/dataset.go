package repo

import (
	"context"
	"errors"
	"fmt"

	"askbase/be/internal/model"

	"gorm.io/gorm"
)

// DatasetRepository 知识库数据访问。
type DatasetRepository struct {
	db *gorm.DB
}

// NewDatasetRepository 创建知识库仓储。
func NewDatasetRepository(db *gorm.DB) *DatasetRepository {
	return &DatasetRepository{db: db}
}

// Create 创建知识库。
func (r *DatasetRepository) Create(ctx context.Context, ds model.Dataset) (model.Dataset, error) {
	if err := conn(ctx, r.db).Create(&ds).Error; err != nil {
		return model.Dataset{}, fmt.Errorf("创建知识库失败: %w", err)
	}
	return ds, nil
}

// GetByUser 按用户与 ID 查询知识库。
func (r *DatasetRepository) GetByUser(ctx context.Context, userID int64, id int64) (model.Dataset, error) {
	var ds model.Dataset
	err := conn(ctx, r.db).
		Where("id = ? AND user_id = ?", id, userID).
		Take(&ds).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Dataset{}, ErrDatasetNotFound
	}
	if err != nil {
		return model.Dataset{}, fmt.Errorf("查询知识库失败: %w", err)
	}
	return ds, nil
}

// GetByID 按 ID 查询知识库。
func (r *DatasetRepository) GetByID(ctx context.Context, id int64) (model.Dataset, error) {
	var ds model.Dataset
	err := conn(ctx, r.db).Where("id = ?", id).Take(&ds).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Dataset{}, ErrDatasetNotFound
	}
	if err != nil {
		return model.Dataset{}, fmt.Errorf("查询知识库失败: %w", err)
	}
	return ds, nil
}

// ListByUser 列出用户知识库，附带文档数与分块数聚合。
func (r *DatasetRepository) ListByUser(ctx context.Context, userID int64) ([]model.Dataset, error) {
	var items []model.Dataset
	if err := conn(ctx, r.db).
		Where("user_id = ?", userID).
		Order("updated_at DESC").
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("查询知识库列表失败: %w", err)
	}
	if len(items) == 0 {
		return []model.Dataset{}, nil
	}

	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	docCounts, err := r.countGroupByDataset(ctx, "t_document", ids)
	if err != nil {
		return nil, err
	}
	chunkCounts, err := r.countGroupByDataset(ctx, "t_chunk", ids)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].DocumentCount = docCounts[items[i].ID]
		items[i].ChunkCount = chunkCounts[items[i].ID]
	}
	return items, nil
}

// countGroupByDataset 按 dataset_id 分组统计行数。
func (r *DatasetRepository) countGroupByDataset(ctx context.Context, table string, datasetIDs []int64) (map[int64]int64, error) {
	type row struct {
		DatasetID int64
		Cnt       int64
	}
	var rows []row
	if err := conn(ctx, r.db).
		Table(table).
		Select("dataset_id, COUNT(*) AS cnt").
		Where("dataset_id IN ?", datasetIDs).
		Group("dataset_id").
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("统计%s数量失败: %w", table, err)
	}
	result := make(map[int64]int64, len(rows))
	for _, item := range rows {
		result[item.DatasetID] = item.Cnt
	}
	return result, nil
}

// Update 按用户更新知识库名称、描述、分块策略与视觉模型。
func (r *DatasetRepository) Update(ctx context.Context, userID int64, id int64, name *string, description *string, chunkStrategy *string, visionModelID *int64, visionModel *string) error {
	updates := map[string]any{}
	if name != nil {
		updates["name"] = *name
	}
	if description != nil {
		updates["description"] = *description
	}
	if chunkStrategy != nil {
		updates["chunk_strategy"] = *chunkStrategy
	}
	if visionModelID != nil {
		updates["vision_model_id"] = *visionModelID
	}
	if visionModel != nil {
		updates["vision_model"] = *visionModel
	}
	if len(updates) == 0 {
		return nil
	}
	res := conn(ctx, r.db).
		Model(&model.Dataset{}).
		Where("id = ? AND user_id = ?", id, userID).
		Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("更新知识库失败: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrDatasetNotFound
	}
	return nil
}

// DeleteByUser 删除知识库。
func (r *DatasetRepository) DeleteByUser(ctx context.Context, userID int64, id int64) error {
	res := conn(ctx, r.db).
		Where("id = ? AND user_id = ?", id, userID).
		Delete(&model.Dataset{})
	if res.Error != nil {
		return fmt.Errorf("删除知识库失败: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrDatasetNotFound
	}
	return nil
}

// CountByEmbedModel 统计用户知识库对该嵌入模型的引用数。
func (r *DatasetRepository) CountByEmbedModel(ctx context.Context, userID int64, embedModelID int64) (int64, error) {
	var n int64
	err := conn(ctx, r.db).
		Model(&model.Dataset{}).
		Where("user_id = ? AND embed_model_id = ?", userID, embedModelID).
		Count(&n).Error
	if err != nil {
		return 0, fmt.Errorf("统计嵌入模型引用失败: %w", err)
	}
	return n, nil
}

// CountByVisionModel 统计用户知识库对该视觉模型的引用数。
func (r *DatasetRepository) CountByVisionModel(ctx context.Context, userID int64, visionModelID int64) (int64, error) {
	var n int64
	err := conn(ctx, r.db).
		Model(&model.Dataset{}).
		Where("user_id = ? AND vision_model_id = ?", userID, visionModelID).
		Count(&n).Error
	if err != nil {
		return 0, fmt.Errorf("统计视觉模型引用失败: %w", err)
	}
	return n, nil
}

// ListUnboundUserIDs 列出仍有未绑定嵌入模型的知识库的用户。
func (r *DatasetRepository) ListUnboundUserIDs(ctx context.Context) ([]int64, error) {
	var ids []int64
	err := conn(ctx, r.db).
		Model(&model.Dataset{}).
		Where("embed_model_id IS NULL OR embed_model_id = 0").
		Distinct("user_id").
		Pluck("user_id", &ids).Error
	if err != nil {
		return nil, fmt.Errorf("查询未绑定知识库失败: %w", err)
	}
	return ids, nil
}

// BindMissingEmbedModel 把用户未绑定的知识库指到指定嵌入模型。
func (r *DatasetRepository) BindMissingEmbedModel(ctx context.Context, userID int64, embedModelID int64, embedName string) error {
	res := conn(ctx, r.db).
		Model(&model.Dataset{}).
		Where("user_id = ? AND (embed_model_id IS NULL OR embed_model_id = 0)", userID).
		Updates(map[string]any{"embed_model_id": embedModelID, "embed_model": embedName})
	if res.Error != nil {
		return fmt.Errorf("回填知识库嵌入模型失败: %w", res.Error)
	}
	return nil
}
