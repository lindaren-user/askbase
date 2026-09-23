package model

import "time"

const (
	SessionStatusActive   int16 = 1 // 进行中
	SessionStatusArchived int16 = 2 // 已归档
)

// Session 对应 t_session。
type Session struct {
	ID             int64     `json:"id" gorm:"primaryKey"`
	UserID         int64     `json:"userId"`
	DatasetID      int64     `json:"datasetId"`
	Title          string    `json:"title"`
	Status         int16     `json:"status"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	DatasetName    string    `json:"datasetName,omitempty" gorm:"->"`
	DatasetDeleted bool      `json:"datasetDeleted" gorm:"-"`
}

// TableName 指定会话表名。
func (Session) TableName() string {
	return "t_session"
}

// CreateSessionRequest 新建会话。
type CreateSessionRequest struct {
	DatasetID int64  `json:"datasetId"`
	Title     string `json:"title"`
}

// UpdateSessionRequest 更新会话标题或归档状态。
type UpdateSessionRequest struct {
	Title  *string `json:"title"`
	Status *string `json:"status"` // "active" / "archived"
}
