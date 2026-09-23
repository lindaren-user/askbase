package model

import "time"

const (
	UserStatusNormal   int16 = 1 // 正常
	UserStatusDisabled int16 = 2 // 禁用
)

// User 对应 t_user。
type User struct {
	ID        int64     `json:"id" gorm:"primaryKey"`
	Email     string    `json:"email"`
	Nickname  string    `json:"nickname"`
	Status    int16     `json:"status"` // 1=正常，2=禁用
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// TableName 指定用户表名。
func (User) TableName() string {
	return "t_user"
}

// TurnstileConfigResponse 前端渲染人机验证所需配置。
type TurnstileConfigResponse struct {
	SiteKey string `json:"siteKey"`
	Enabled bool   `json:"enabled"`
}
