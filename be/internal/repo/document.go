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

// DocumentRepository 文档数据访问。
type DocumentRepository struct {
	db *gorm.DB
}

// NewDocumentRepository 创建文档仓储。
func NewDocumentRepository(db *gorm.DB) *DocumentRepository {
	return &DocumentRepository{db: db}
}

// Create 登记文档；同知识库内 file_hash 冲突时返回 ErrDocumentExists 与已存在文档。
func (r *DocumentRepository) Create(ctx context.Context, doc model.Document) (model.Document, error) {
	if doc.FileHash != "" {
		var existing model.Document
		err := conn(ctx, r.db).
			Where("dataset_id = ? AND file_hash = ?", doc.DatasetID, doc.FileHash).
			Take(&existing).Error
		if err == nil {
			return existing, ErrDocumentExists
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return model.Document{}, fmt.Errorf("查询文档哈希失败: %w", err)
		}
	}
	create := conn(ctx, r.db)
	if doc.FileHash != "" {
		// DO NOTHING 避免并发唯一冲突使 PostgreSQL 事务进入 aborted 状态，
		// 从而保证调用方仍可在同一事务内安全创建 Outbox。
		create = create.Clauses(clause.OnConflict{DoNothing: true})
	}
	result := create.Create(&doc)
	if result.Error != nil {
		return model.Document{}, fmt.Errorf("登记文档失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		var existing model.Document
		if err := conn(ctx, r.db).
			Where("dataset_id = ? AND file_hash = ?", doc.DatasetID, doc.FileHash).
			Take(&existing).Error; err != nil {
			return model.Document{}, fmt.Errorf("查询并发登记的文档失败: %w", err)
		}
		return existing, ErrDocumentExists
	}
	return doc, nil
}

// GetByID 按 ID 查询文档。
func (r *DocumentRepository) GetByID(ctx context.Context, id int64) (model.Document, error) {
	var doc model.Document
	err := conn(ctx, r.db).Where("id = ?", id).Take(&doc).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Document{}, ErrDocumentNotFound
	}
	if err != nil {
		return model.Document{}, fmt.Errorf("查询文档失败: %w", err)
	}
	return doc, nil
}

// ListByDataset 列出知识库文档，附带分块数。
func (r *DocumentRepository) ListByDataset(ctx context.Context, datasetID int64) ([]model.Document, error) {
	var items []model.Document
	if err := conn(ctx, r.db).
		Where("dataset_id = ?", datasetID).
		Order("created_at DESC").
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("查询文档列表失败: %w", err)
	}
	if len(items) == 0 {
		return []model.Document{}, nil
	}
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	type row struct {
		DocumentID int64
		Cnt        int64
	}
	var rows []row
	if err := conn(ctx, r.db).
		Table("t_chunk").
		Select("document_id, COUNT(*) AS cnt").
		Where("document_id IN ?", ids).
		Group("document_id").
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("统计文档分块数失败: %w", err)
	}
	counts := make(map[int64]int64, len(rows))
	for _, item := range rows {
		counts[item.DocumentID] = item.Cnt
	}
	for i := range items {
		items[i].ChunkCount = counts[items[i].ID]
	}
	return items, nil
}

// Update 更新文档名称与启停；RowsAffected=0 时返回 ErrDocumentNotFound。
func (r *DocumentRepository) Update(ctx context.Context, id int64, name *string, enabled *bool) error {
	updates := map[string]any{}
	if name != nil {
		updates["name"] = *name
	}
	if enabled != nil {
		updates["enabled"] = *enabled
	}
	if len(updates) == 0 {
		return nil
	}
	res := conn(ctx, r.db).Model(&model.Document{}).Where("id = ?", id).Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("更新文档失败: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrDocumentNotFound
	}
	return nil
}

// UpdateStatus 推进文档状态机与进度；parseError 为空串则清 NULL。
func (r *DocumentRepository) UpdateStatus(ctx context.Context, id int64, status string, progress int16, parseError string) error {
	updates := map[string]any{
		"status":      status,
		"progress":    progress,
		"parse_error": nullIfBlank(parseError),
	}
	res := conn(ctx, r.db).Model(&model.Document{}).Where("id = ?", id).Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("更新文档状态失败: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrDocumentNotFound
	}
	return nil
}

// ResetForParse 按调用方给出的状态递增解析版本并清理上次尝试结果。
func (r *DocumentRepository) ResetForParse(ctx context.Context, id int64, status string, progress int16) (model.Document, error) {
	res := conn(ctx, r.db).Model(&model.Document{}).Where("id = ?", id).Updates(map[string]any{
		"status":          status,
		"progress":        progress,
		"parse_error":     nil,
		"parse_version":   gorm.Expr("parse_version + 1"),
		"attempt_count":   0,
		"last_error":      nil,
		"last_attempt_at": nil,
	})
	if res.Error != nil {
		return model.Document{}, fmt.Errorf("重置文档解析状态失败: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return model.Document{}, ErrDocumentNotFound
	}
	return r.GetByID(ctx, id)
}

// RecordParseAttempt 按调用方给出的时间记录当前解析版本的最近一次尝试结果。
func (r *DocumentRepository) RecordParseAttempt(ctx context.Context, id int64, parseVersion int64, attempt int, attemptedAt time.Time, lastError string) (bool, error) {
	updates := map[string]any{
		// 当前仅实现业务幂等，消息幂等依旧存在。
		// RabbitMQ 重复投递或重试消息乱序到达时，旧 attempt 不得覆盖已经记录的较新次数。
		"attempt_count":   gorm.Expr("GREATEST(attempt_count, ?)", attempt),
		"last_attempt_at": attemptedAt,
		"last_error":      nullIfBlank(lastError),
	}
	res := conn(ctx, r.db).Model(&model.Document{}).
		Where("id = ? AND parse_version = ?", id, parseVersion).
		Updates(updates)
	if res.Error != nil {
		return false, fmt.Errorf("记录文档解析尝试失败: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// UpdateStatusVersion 按解析版本更新终态，避免旧消息覆盖新任务。
func (r *DocumentRepository) UpdateStatusVersion(ctx context.Context, id int64, parseVersion int64, status string, progress int16, parseError string) (bool, error) {
	res := conn(ctx, r.db).Model(&model.Document{}).
		Where("id = ? AND parse_version = ?", id, parseVersion).
		Updates(map[string]any{
			"status":      status,
			"progress":    progress,
			"parse_error": nullIfBlank(parseError),
		})
	if res.Error != nil {
		return false, fmt.Errorf("按版本更新文档状态失败: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// UpdateStatusVersionUnless 按解析版本更新状态，并排除调用方指定的当前状态。
func (r *DocumentRepository) UpdateStatusVersionUnless(ctx context.Context, id int64, parseVersion int64, excludedStatuses []string, status string, progress int16) (bool, error) {
	query := conn(ctx, r.db).Model(&model.Document{}).Where("id = ? AND parse_version = ?", id, parseVersion)
	if len(excludedStatuses) > 0 {
		query = query.Where("status NOT IN ?", excludedStatuses)
	}
	res := query.
		Updates(map[string]any{
			"status":      status,
			"progress":    progress,
			"parse_error": nil,
		})
	if res.Error != nil {
		return false, fmt.Errorf("按版本条件更新文档状态失败: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// UpdateEnabledByIDs 批量更新文档启停。
func (r *DocumentRepository) UpdateEnabledByIDs(ctx context.Context, ids []int64, enabled bool) error {
	if len(ids) == 0 {
		return nil
	}
	if err := conn(ctx, r.db).Model(&model.Document{}).Where("id IN ?", ids).Update("enabled", enabled).Error; err != nil {
		return fmt.Errorf("批量更新文档启停失败: %w", err)
	}
	return nil
}

// Delete 删除文档。
func (r *DocumentRepository) Delete(ctx context.Context, id int64) error {
	res := conn(ctx, r.db).Where("id = ?", id).Delete(&model.Document{})
	if res.Error != nil {
		return fmt.Errorf("删除文档失败: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrDocumentNotFound
	}
	return nil
}

// ListNamesByIDs 批量取文档名（引用溯源展示用）。
func (r *DocumentRepository) ListNamesByIDs(ctx context.Context, ids []int64) (map[int64]string, error) {
	if len(ids) == 0 {
		return map[int64]string{}, nil
	}
	var docs []model.Document
	if err := conn(ctx, r.db).Select("id", "name").Where("id IN ?", ids).Find(&docs).Error; err != nil {
		return nil, fmt.Errorf("批量查询文档名失败: %w", err)
	}
	names := make(map[int64]string, len(docs))
	for _, doc := range docs {
		names[doc.ID] = doc.Name
	}
	return names, nil
}

// ListR2KeysByDataset 列出知识库下全部文档的对象 key，用于删除知识库时清理。
func (r *DocumentRepository) ListR2KeysByDataset(ctx context.Context, datasetID int64) ([]string, error) {
	var keys []string
	if err := conn(ctx, r.db).
		Model(&model.Document{}).
		Where("dataset_id = ? AND r2_key <> ''", datasetID).
		Pluck("r2_key", &keys).Error; err != nil {
		return nil, fmt.Errorf("查询文档对象键失败: %w", err)
	}
	return orEmpty(keys), nil
}

// DeleteByDataset 删除知识库下全部文档，返回删除的文档 ID。
func (r *DocumentRepository) DeleteByDataset(ctx context.Context, datasetID int64) ([]int64, error) {
	var ids []int64
	if err := conn(ctx, r.db).
		Model(&model.Document{}).
		Where("dataset_id = ?", datasetID).
		Pluck("id", &ids).Error; err != nil {
		return nil, fmt.Errorf("查询文档 ID 失败: %w", err)
	}
	if len(ids) == 0 {
		return []int64{}, nil
	}
	if err := conn(ctx, r.db).Where("dataset_id = ?", datasetID).Delete(&model.Document{}).Error; err != nil {
		return nil, fmt.Errorf("删除知识库文档失败: %w", err)
	}
	return ids, nil
}

// nullIfBlank 空白字符串写成 SQL NULL，非空则原样写入。
func nullIfBlank(s string) any {
	if s == "" {
		return nil
	}
	return s
}
