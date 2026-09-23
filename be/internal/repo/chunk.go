package repo

import (
	"context"
	"errors"
	"fmt"

	"askbase/be/internal/model"

	"gorm.io/gorm"
)

// retrievalReadySQL 仅已启用且解析完成的文档分块可被召回。
const retrievalReadySQL = "c.enabled AND d.enabled AND d.status = '" + model.DocumentStatusDone + "'"

// ChunkRepository 分块数据访问。
type ChunkRepository struct {
	db *gorm.DB
}

// NewChunkRepository 创建分块仓储。
func NewChunkRepository(db *gorm.DB) *ChunkRepository {
	return &ChunkRepository{db: db}
}

// BulkInsert 批量写入分块。
func (r *ChunkRepository) BulkInsert(ctx context.Context, chunks []model.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}
	if err := conn(ctx, r.db).Create(&chunks).Error; err != nil {
		return fmt.Errorf("批量写入分块失败: %w", err)
	}
	return nil
}

// ListByDocument 按文档列出分块，按块序号排序。
func (r *ChunkRepository) ListByDocument(ctx context.Context, documentID int64) ([]model.Chunk, error) {
	var items []model.Chunk
	if err := conn(ctx, r.db).
		Select("id", "document_id", "dataset_id", "chunk_index", "content", "token_num", "enabled", "image_key", "element_type", "page_number", "bbox", "parser_name", "created_at").
		Where("document_id = ?", documentID).
		Order("chunk_index ASC").
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("查询分块列表失败: %w", err)
	}
	return orEmpty(items), nil
}

// GetByID 按 ID 查询分块。
func (r *ChunkRepository) GetByID(ctx context.Context, id int64) (model.Chunk, error) {
	var chunk model.Chunk
	err := conn(ctx, r.db).Where("id = ?", id).Take(&chunk).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Chunk{}, ErrChunkNotFound
	}
	if err != nil {
		return model.Chunk{}, fmt.Errorf("查询分块失败: %w", err)
	}
	return chunk, nil
}

// Update 更新分块内容与启停。
func (r *ChunkRepository) Update(ctx context.Context, id int64, enabled *bool, content *string, tokenNum *int) error {
	updates := map[string]any{}
	if enabled != nil {
		updates["enabled"] = *enabled
	}
	if content != nil {
		updates["content"] = *content
	}
	if tokenNum != nil {
		updates["token_num"] = *tokenNum
	}
	if len(updates) == 0 {
		return nil
	}
	res := conn(ctx, r.db).Model(&model.Chunk{}).Where("id = ?", id).Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("更新分块失败: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrChunkNotFound
	}
	return nil
}

// DeleteByDocument 删除文档全部分块。
func (r *ChunkRepository) DeleteByDocument(ctx context.Context, documentID int64) error {
	if err := conn(ctx, r.db).Where("document_id = ?", documentID).Delete(&model.Chunk{}).Error; err != nil {
		return fmt.Errorf("删除文档分块失败: %w", err)
	}
	return nil
}

// DeleteByDataset 删除知识库全部分块。
func (r *ChunkRepository) DeleteByDataset(ctx context.Context, datasetID int64) error {
	if err := conn(ctx, r.db).Where("dataset_id = ?", datasetID).Delete(&model.Chunk{}).Error; err != nil {
		return fmt.Errorf("删除知识库分块失败: %w", err)
	}
	return nil
}

// SearchByFullText 全文检索：使用 GIN 倒排索引召回，再按覆盖密度排序。
func (r *ChunkRepository) SearchByFullText(ctx context.Context, datasetID int64, query string, topK int) ([]model.RetrievalHit, error) {
	var hits []model.RetrievalHit
	err := conn(ctx, r.db).Raw(`
		SELECT c.id AS chunk_id, c.document_id, d.name AS document_name, c.content, c.image_key,
		       c.element_type, c.page_number, c.bbox, c.parser_name,
		       ts_rank_cd(c.search_vector, q.query) AS score
		FROM t_chunk c
		JOIN t_document d ON d.id = c.document_id
		CROSS JOIN LATERAL (SELECT askbase_search_query(?) AS query) q
		WHERE c.dataset_id = ? AND `+retrievalReadySQL+`
		  AND q.query IS NOT NULL
		  AND c.search_vector @@ q.query
		ORDER BY score DESC
		LIMIT ?`,
		query, datasetID, topK,
	).Scan(&hits).Error
	if err != nil {
		return nil, fmt.Errorf("全文检索失败: %w", err)
	}
	return orEmpty(hits), nil
}

// ListByIDs 按 ID 批量取分块（引用溯源用）。
func (r *ChunkRepository) ListByIDs(ctx context.Context, ids []int64) ([]model.Chunk, error) {
	if len(ids) == 0 {
		return []model.Chunk{}, nil
	}
	var items []model.Chunk
	if err := conn(ctx, r.db).
		Select("id", "document_id", "dataset_id", "chunk_index", "content", "token_num", "enabled", "image_key", "element_type", "page_number", "bbox", "parser_name").
		Where("id IN ?", ids).
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("批量查询分块失败: %w", err)
	}
	return orEmpty(items), nil
}

// ListImageKeysByDocument 列出文档分块上的图片对象 key。
func (r *ChunkRepository) ListImageKeysByDocument(ctx context.Context, documentID int64) ([]string, error) {
	var keys []string
	err := conn(ctx, r.db).
		Model(&model.Chunk{}).
		Where("document_id = ? AND image_key <> ''", documentID).
		Pluck("image_key", &keys).Error
	if err != nil {
		return nil, fmt.Errorf("查询分块图片失败: %w", err)
	}
	return orEmpty(keys), nil
}

// ListImageKeysByDataset 列出知识库分块上的图片对象 key。
func (r *ChunkRepository) ListImageKeysByDataset(ctx context.Context, datasetID int64) ([]string, error) {
	var keys []string
	err := conn(ctx, r.db).
		Model(&model.Chunk{}).
		Where("dataset_id = ? AND image_key <> ''", datasetID).
		Pluck("image_key", &keys).Error
	if err != nil {
		return nil, fmt.Errorf("查询知识库分块图片失败: %w", err)
	}
	return orEmpty(keys), nil
}
