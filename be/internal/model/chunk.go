package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Float64Array 以 JSONB 保存坐标数组，并实现 GORM Value 与 Scan 接口。
type Float64Array []float64

// Value 把坐标数组编码为数据库 JSON 值。
func (v Float64Array) Value() (driver.Value, error) {
	if v == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(v)
}

// Scan 从 PostgreSQL JSONB 解码坐标数组。
func (v *Float64Array) Scan(src any) error {
	var data []byte
	switch value := src.(type) {
	case nil:
		*v = Float64Array{}
		return nil
	case []byte:
		data = value
	case string:
		data = []byte(value)
	default:
		return fmt.Errorf("无法解析 bbox 类型 %T", src)
	}
	return json.Unmarshal(data, v)
}

// Chunk 对应 t_chunk。
type Chunk struct {
	ID          int64        `json:"id,string" gorm:"primaryKey"` // 哈希 ID，JSON 用字符串以免 JS 精度丢失
	DocumentID  int64        `json:"documentId"`
	DatasetID   int64        `json:"datasetId"`
	ChunkIndex  int          `json:"chunkIndex"`
	Content     string       `json:"content"`
	TokenNum    int          `json:"tokenNum"`
	Enabled     bool         `json:"enabled"`
	ImageKey    string       `json:"-" gorm:"column:image_key"`
	ImageURL    string       `json:"imageUrl,omitempty" gorm:"-"`
	ElementType string       `json:"elementType" gorm:"column:element_type"`
	PageNumber  int          `json:"pageNumber" gorm:"column:page_number"`
	BBox        Float64Array `json:"bbox" gorm:"column:bbox;type:jsonb"`
	ParserName  string       `json:"parserName" gorm:"column:parser_name"`
	CreatedAt   time.Time    `json:"createdAt"`
	UpdatedAt   time.Time    `json:"updatedAt"`
}

// TableName 指定分块表名。
func (Chunk) TableName() string {
	return "t_chunk"
}

// EmbedText 向量化输入：文档名作为检索信号拼在正文前。
func EmbedText(documentName, content string) string {
	name := strings.TrimSpace(documentName)
	if name == "" {
		return content
	}
	return name + "\n" + content
}

// UpdateChunkRequest 更新分块；指针字段区分未传与空值。
type UpdateChunkRequest struct {
	Enabled *bool   `json:"enabled"`
	Content *string `json:"content"`
}

// RetrievalHit 检索命中的分块及相似度。
type RetrievalHit struct {
	ChunkID      int64        `json:"chunkId,string"`
	DocumentID   int64        `json:"documentId"`
	DocumentName string       `json:"documentName"`
	Content      string       `json:"content"`
	Score        float64      `json:"score"` // 余弦相似度 1-distance
	ImageKey     string       `json:"-"`
	ImageURL     string       `json:"imageUrl,omitempty"`
	ElementType  string       `json:"elementType"`
	PageNumber   int          `json:"pageNumber"`
	BBox         Float64Array `json:"bbox"`
	ParserName   string       `json:"parserName"`
}

// RetrievalTestRequest 检索测试请求。
type RetrievalTestRequest struct {
	DatasetID int64   `json:"datasetId"`
	Query     string  `json:"query"`
	TopK      int     `json:"topK"`
	MinScore  float64 `json:"minScore"`
}
