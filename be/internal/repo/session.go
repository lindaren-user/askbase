package repo

import (
	"context"
	"errors"
	"fmt"

	"askbase/be/internal/model"

	"gorm.io/gorm"
)

// SessionRepository 会话数据访问。
type SessionRepository struct {
	db *gorm.DB
}

// NewSessionRepository 创建会话仓储。
func NewSessionRepository(db *gorm.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

// Create 创建进行中的会话。
func (r *SessionRepository) Create(ctx context.Context, userID int64, datasetID int64, title string) (model.Session, error) {
	session := model.Session{
		UserID:    userID,
		DatasetID: datasetID,
		Title:     title,
		Status:    model.SessionStatusActive,
	}
	if err := conn(ctx, r.db).Create(&session).Error; err != nil {
		return model.Session{}, fmt.Errorf("创建会话失败: %w", err)
	}
	return session, nil
}

// GetByUser 按用户与 ID 查询会话。
func (r *SessionRepository) GetByUser(ctx context.Context, userID int64, id int64) (model.Session, error) {
	var session model.Session
	err := conn(ctx, r.db).Where("id = ? AND user_id = ?", id, userID).Take(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Session{}, ErrSessionNotFound
	}
	if err != nil {
		return model.Session{}, fmt.Errorf("查询会话失败: %w", err)
	}
	return session, nil
}

// ListByUser 列出用户会话（含知识库名）；datasetID 非空时按知识库过滤；status 非 0 时按状态过滤。
func (r *SessionRepository) ListByUser(ctx context.Context, userID int64, datasetID *int64, status int16) ([]model.Session, error) {
	q := conn(ctx, r.db).
		Table("t_session").
		Select("t_session.*, t_dataset.name AS dataset_name").
		Joins("LEFT JOIN t_dataset ON t_dataset.id = t_session.dataset_id").
		Where("t_session.user_id = ?", userID)
	if datasetID != nil {
		q = q.Where("t_session.dataset_id = ?", *datasetID)
	}
	if status != 0 {
		q = q.Where("t_session.status = ?", status)
	}
	var items []model.Session
	if err := q.Order("t_session.updated_at DESC").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("查询会话列表失败: %w", err)
	}
	return orEmpty(items), nil
}

// ListArchivedByUser 列出用户已归档会话，附带知识库名称。
func (r *SessionRepository) ListArchivedByUser(ctx context.Context, userID int64) ([]model.Session, error) {
	var items []model.Session
	err := conn(ctx, r.db).
		Table("t_session").
		Select("t_session.*, t_dataset.name AS dataset_name").
		Joins("LEFT JOIN t_dataset ON t_dataset.id = t_session.dataset_id").
		Where("t_session.user_id = ? AND t_session.status = ?", userID, model.SessionStatusArchived).
		Order("t_session.updated_at DESC").
		Find(&items).Error
	if err != nil {
		return nil, fmt.Errorf("查询归档会话失败: %w", err)
	}
	return orEmpty(items), nil
}

// Update 更新会话标题或状态。
func (r *SessionRepository) Update(ctx context.Context, userID int64, id int64, title *string, status *int16) error {
	updates := map[string]any{}
	if title != nil {
		updates["title"] = *title
	}
	if status != nil {
		updates["status"] = *status
	}
	if len(updates) == 0 {
		return nil
	}
	res := conn(ctx, r.db).Model(&model.Session{}).Where("id = ? AND user_id = ?", id, userID).Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("更新会话失败: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// Touch 刷新会话更新时间（新消息时置顶）。
func (r *SessionRepository) Touch(ctx context.Context, id int64) error {
	res := conn(ctx, r.db).Model(&model.Session{}).Where("id = ?", id).Update("updated_at", gorm.Expr("now()"))
	if res.Error != nil {
		return fmt.Errorf("刷新会话时间失败: %w", res.Error)
	}
	return nil
}

// DeleteByUser 删除会话。
func (r *SessionRepository) DeleteByUser(ctx context.Context, userID int64, id int64) error {
	res := conn(ctx, r.db).Where("id = ? AND user_id = ?", id, userID).Delete(&model.Session{})
	if res.Error != nil {
		return fmt.Errorf("删除会话失败: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// DeleteAllByUser 删除用户全部会话。
func (r *SessionRepository) DeleteAllByUser(ctx context.Context, userID int64) error {
	if err := conn(ctx, r.db).Where("user_id = ?", userID).Delete(&model.Session{}).Error; err != nil {
		return fmt.Errorf("删除用户会话失败: %w", err)
	}
	return nil
}
