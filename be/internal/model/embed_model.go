package model

import (
	"strings"
	"time"
)

const OfficialEmbedName = "服务端默认"

// OfficialVisionName 服务端默认视觉模型展示名，不可删除。
const OfficialVisionName = OfficialEmbedName

// OfficialModelID 服务端默认模型的虚拟 ID，不入库，解析时读 config。
const OfficialModelID int64 = 0

// UnusedVisionModelID 知识库不使用视觉模型。
const UnusedVisionModelID int64 = -1

// EmbedModel 对应 t_embed_model。API Key 不进 JSON。
type EmbedModel struct {
	ID        int64     `json:"id" gorm:"primaryKey"`
	UserID    int64     `json:"userId"`
	Name      string    `json:"name"`
	ModelID   string    `json:"modelId"`
	APIURL    string    `json:"apiUrl"`
	APIKey    string    `json:"-"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// TableName 指定嵌入模型表名。
func (EmbedModel) TableName() string {
	return "t_embed_model"
}

// EmbedModelView 列表/详情：Key 仅掩码。
type EmbedModelView struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	ModelID      string `json:"modelId"`
	APIURL       string `json:"apiUrl"`
	APIKeyMasked string `json:"apiKeyMasked"`
}

// ToView 转对外视图。
func (m EmbedModel) ToView() EmbedModelView {
	return EmbedModelView{
		ID:           m.ID,
		Name:         m.Name,
		ModelID:      m.ModelID,
		APIURL:       m.APIURL,
		APIKeyMasked: maskAPIKey(m.APIKey),
	}
}

func maskAPIKey(key string) string {
	runes := []rune(strings.TrimSpace(key))
	if len(runes) == 0 {
		return ""
	}
	if len(runes) <= 4 {
		return "****"
	}
	return "****" + string(runes[len(runes)-4:])
}

// CreateEmbedModelRequest 登记嵌入模型。
type CreateEmbedModelRequest struct {
	Name    string `json:"name"`
	ModelID string `json:"modelId"`
	APIURL  string `json:"apiUrl"`
	APIKey  string `json:"apiKey"`
}

// UpdateEmbedModelRequest 更新嵌入模型；指针区分未传。
type UpdateEmbedModelRequest struct {
	Name    *string `json:"name"`
	ModelID *string `json:"modelId"`
	APIURL  *string `json:"apiUrl"`
	APIKey  *string `json:"apiKey"`
}
