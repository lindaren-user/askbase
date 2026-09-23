package model

import "time"

// 知识库解析模板常量；模板决定文档清洗、结构识别与分块方式。
const (
	ChunkStrategyGeneral = "general" // 通用文档
	ChunkStrategyBook    = "book"    // 书籍、教材
	ChunkStrategyPaper   = "paper"   // 学术论文
	ChunkStrategyResume  = "resume"  // 简历
	ChunkStrategyQA      = "qa"      // 问答对、FAQ
)

// IsChunkStrategy 判断是否为当前支持的知识库解析模板。
func IsChunkStrategy(strategy string) bool {
	switch strategy {
	case ChunkStrategyGeneral, ChunkStrategyBook, ChunkStrategyPaper, ChunkStrategyResume, ChunkStrategyQA:
		return true
	default:
		return false
	}
}

// Dataset 对应 t_dataset。
type Dataset struct {
	ID            int64     `json:"id" gorm:"primaryKey"`
	UserID        int64     `json:"userId"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	ChunkStrategy string    `json:"chunkStrategy"`
	EmbedModel    string    `json:"embedModel"`
	EmbedModelID  int64     `json:"embedModelId"`
	VisionModel   string    `json:"visionModel"`
	VisionModelID int64     `json:"visionModelId"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`

	// 列表聚合字段，非表列
	DocumentCount int64 `json:"documentCount" gorm:"-"`
	ChunkCount    int64 `json:"chunkCount" gorm:"-"`
}

// TableName 指定知识库表名。
func (Dataset) TableName() string {
	return "t_dataset"
}

// CreateDatasetRequest 新建知识库。
type CreateDatasetRequest struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	ChunkStrategy string `json:"chunkStrategy"`
	EmbedModelID  int64  `json:"embedModelId"`
	VisionModelID int64  `json:"visionModelId"`
}

// UpdateDatasetRequest 更新知识库；指针字段区分未传与空值。
type UpdateDatasetRequest struct {
	Name          *string `json:"name"`
	Description   *string `json:"description"`
	ChunkStrategy *string `json:"chunkStrategy"`
	VisionModelID *int64  `json:"visionModelId"`
}
