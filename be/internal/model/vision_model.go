package model

import "time"

// VisionModel 对应 t_vision_model。API Key 不进 JSON。
type VisionModel struct {
	ID        int64     `json:"id" gorm:"primaryKey"`
	UserID    int64     `json:"userId"`
	Name      string    `json:"name"`
	ModelID   string    `json:"modelId"`
	APIURL    string    `json:"apiUrl"`
	APIKey    string    `json:"-"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// TableName 指定视觉模型表名。
func (VisionModel) TableName() string {
	return "t_vision_model"
}

// VisionModelView 列表/详情：Key 仅掩码。
type VisionModelView struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	ModelID      string `json:"modelId"`
	APIURL       string `json:"apiUrl"`
	APIKeyMasked string `json:"apiKeyMasked"`
}

// ToView 转对外视图。
func (m VisionModel) ToView() VisionModelView {
	return VisionModelView{
		ID:           m.ID,
		Name:         m.Name,
		ModelID:      m.ModelID,
		APIURL:       m.APIURL,
		APIKeyMasked: maskAPIKey(m.APIKey),
	}
}

// CreateVisionModelRequest 登记视觉模型。
type CreateVisionModelRequest struct {
	Name    string `json:"name"`
	ModelID string `json:"modelId"`
	APIURL  string `json:"apiUrl"`
	APIKey  string `json:"apiKey"`
}

// UpdateVisionModelRequest 更新视觉模型；指针区分未传。
type UpdateVisionModelRequest struct {
	Name    *string `json:"name"`
	ModelID *string `json:"modelId"`
	APIURL  *string `json:"apiUrl"`
	APIKey  *string `json:"apiKey"`
}
