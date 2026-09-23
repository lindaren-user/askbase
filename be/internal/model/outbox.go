package model

import (
	"encoding/json"
	"time"
)

const (
	// OutboxTopicDocumentParse 表示文档解析任务。
	OutboxTopicDocumentParse = "document.parse"

	// OutboxStatusPending 表示事件尚未成功发布。
	OutboxStatusPending = "pending"
	// OutboxStatusPublished 表示事件已收到 RabbitMQ publisher confirm。
	OutboxStatusPublished = "published"
)

// Outbox 对应 t_outbox 中的一条待发布事件。
type Outbox struct {
	ID              int64           `gorm:"primaryKey"`
	Topic           string          `gorm:"size:128;not null"`
	AggregateID     int64           `gorm:"not null"`
	Payload         json.RawMessage `gorm:"type:jsonb;not null"`
	Status          string          `gorm:"size:16;not null"`
	PublishAttempts int             `gorm:"not null"`
	NextAttemptAt   time.Time       `gorm:"not null"`
	PublishedAt     *time.Time
	LastError       *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// TableName 指定事务 Outbox 表名。
func (Outbox) TableName() string {
	return "t_outbox"
}

// DocumentParseEvent 是 RabbitMQ 文档解析消息体。
type DocumentParseEvent struct {
	EventID      int64 `json:"eventId,omitempty"`
	DocumentID   int64 `json:"documentId"`
	ParseVersion int64 `json:"parseVersion"`
	Attempt      int   `json:"attempt"`
}
