package repo

import (
	"context"
	"fmt"

	"askbase/be/internal/model"
	"askbase/be/internal/tx"

	"gorm.io/gorm"
)

// MessageRepository 消息数据访问。
type MessageRepository struct {
	db *gorm.DB
}

// NewMessageRepository 创建消息仓储。
func NewMessageRepository(db *gorm.DB) *MessageRepository {
	return &MessageRepository{db: db}
}

// Create 写入消息。
func (r *MessageRepository) Create(ctx context.Context, msg model.Message) (model.Message, error) {
	if err := conn(ctx, r.db).Create(&msg).Error; err != nil {
		return model.Message{}, fmt.Errorf("写入消息失败: %w", err)
	}
	return msg, nil
}

// ListBySession 按会话列出消息，按时间升序。
func (r *MessageRepository) ListBySession(ctx context.Context, sessionID int64) ([]model.Message, error) {
	var items []model.Message
	if err := conn(ctx, r.db).
		Where("session_id = ?", sessionID).
		Order("id ASC").
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("查询消息列表失败: %w", err)
	}
	return orEmpty(items), nil
}

// ListCitationsByMessageIDs 批量读取消息引用，并按消息和引用编号排序。
func (r *MessageRepository) ListCitationsByMessageIDs(ctx context.Context, messageIDs []int64) ([]model.MessageCitation, error) {
	if len(messageIDs) == 0 {
		return []model.MessageCitation{}, nil
	}
	var items []model.MessageCitation
	if err := conn(ctx, r.db).
		Where("message_id IN ?", messageIDs).
		Order("message_id ASC, position ASC").
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("查询消息引用失败: %w", err)
	}
	return orEmpty(items), nil
}

// UpdateContent 流式结束后原子回填 assistant 消息内容与引用元数据。
func (r *MessageRepository) UpdateContent(ctx context.Context, id int64, content string, citations []model.Citation) error {
	return tx.With(ctx, r.db, func(ctx context.Context) error {
		res := conn(ctx, r.db).Model(&model.Message{}).Where("id = ?", id).Update("content", content)
		if res.Error != nil {
			return fmt.Errorf("回填消息内容失败: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("回填消息内容失败: 消息不存在")
		}
		if err := conn(ctx, r.db).Where("message_id = ?", id).Delete(&model.MessageCitation{}).Error; err != nil {
			return fmt.Errorf("清理消息引用失败: %w", err)
		}
		if len(citations) == 0 {
			return nil
		}
		items := make([]model.MessageCitation, len(citations))
		for i, citation := range citations {
			items[i] = model.MessageCitation{
				MessageID: id,
				Position:  citation.N,
				ChunkID:   citation.ChunkID,
				Score:     citation.Score,
			}
		}
		if err := conn(ctx, r.db).Create(&items).Error; err != nil {
			return fmt.Errorf("写入消息引用失败: %w", err)
		}
		return nil
	})
}

// DeleteBySession 删除会话全部消息。
func (r *MessageRepository) DeleteBySession(ctx context.Context, sessionID int64) error {
	return tx.With(ctx, r.db, func(ctx context.Context) error {
		messageIDs := conn(ctx, r.db).Model(&model.Message{}).Select("id").Where("session_id = ?", sessionID)
		if err := conn(ctx, r.db).Where("message_id IN (?)", messageIDs).Delete(&model.MessageCitation{}).Error; err != nil {
			return fmt.Errorf("删除会话消息引用失败: %w", err)
		}
		if err := conn(ctx, r.db).Where("session_id = ?", sessionID).Delete(&model.Message{}).Error; err != nil {
			return fmt.Errorf("删除会话消息失败: %w", err)
		}
		return nil
	})
}

// DeleteAllByUser 删除用户全部会话的消息。
func (r *MessageRepository) DeleteAllByUser(ctx context.Context, userID int64) error {
	return tx.With(ctx, r.db, func(ctx context.Context) error {
		messageIDs := conn(ctx, r.db).Model(&model.Message{}).
			Select("id").
			Where("session_id IN (SELECT id FROM t_session WHERE user_id = ?)", userID)
		if err := conn(ctx, r.db).Where("message_id IN (?)", messageIDs).Delete(&model.MessageCitation{}).Error; err != nil {
			return fmt.Errorf("删除用户消息引用失败: %w", err)
		}
		if err := conn(ctx, r.db).
			Where("session_id IN (SELECT id FROM t_session WHERE user_id = ?)", userID).
			Delete(&model.Message{}).Error; err != nil {
			return fmt.Errorf("删除用户消息失败: %w", err)
		}
		return nil
	})
}
