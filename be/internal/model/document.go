package model

import "time"

// 文档状态机：pending → parsing → chunking → embedding → done，失败为 failed。
const (
	DocumentStatusPending    = "pending"
	DocumentStatusParsing    = "parsing"
	DocumentStatusChunking   = "chunking"
	DocumentStatusEmbedding  = "embedding"
	DocumentStatusDone       = "done"
	DocumentStatusFailed     = "failed"
	DocumentStatusCancelling = "cancelling" // 用户请求停止，worker 在阶段边界退出
	DocumentStatusCancelled  = "cancelled"
)

// Document 对应 t_document。
type Document struct {
	ID            int64      `json:"id" gorm:"primaryKey"`
	DatasetID     int64      `json:"datasetId"`
	Name          string     `json:"name"`
	R2Key         string     `json:"r2Key"`
	FileHash      string     `json:"fileHash"`
	SizeBytes     int64      `json:"sizeBytes"`
	Status        string     `json:"status"`
	Progress      int16      `json:"progress"`
	ParseError    *string    `json:"parseError,omitempty"`
	ParseVersion  int64      `json:"-"`
	AttemptCount  int        `json:"-"`
	LastError     *string    `json:"-"`
	LastAttemptAt *time.Time `json:"-"`
	Enabled       bool       `json:"enabled"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`

	// 列表聚合字段，非表列
	ChunkCount int64 `json:"chunkCount" gorm:"-"`
}

// TableName 指定文档表名。
func (Document) TableName() string {
	return "t_document"
}

// UploadURLRequest 申请预签名直传地址。
type UploadURLRequest struct {
	DatasetID   int64  `json:"datasetId"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	FileHash    string `json:"fileHash"` // 前端计算的 SHA-256，用于幂等登记
}

// RegisterDocumentRequest 直传完成后登记文档。
type RegisterDocumentRequest struct {
	Name      string `json:"name"`
	R2Key     string `json:"r2Key"`
	FileHash  string `json:"fileHash"`
	SizeBytes int64  `json:"sizeBytes"`
}

// UpdateDocumentRequest 更新文档；指针字段区分未传与空值。
type UpdateDocumentRequest struct {
	Name    *string `json:"name"`
	Enabled *bool   `json:"enabled"`
}

// BatchDocumentStatusRequest 批量启停文档。
type BatchDocumentStatusRequest struct {
	IDs     []int64 `json:"ids"`
	Enabled bool    `json:"enabled"`
}

// DocumentPreview 在线预览：text/markdown 给正文，pdf/image 给预签名 URL。
type DocumentPreview struct {
	Kind    string `json:"kind"` // text | markdown | pdf | image
	Content string `json:"content,omitempty"`
	URL     string `json:"url,omitempty"`
	Name    string `json:"name"`
}

// DocumentDownload 预签名下载地址。
type DocumentDownload struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
}

// DocumentProgress 文档解析进度（轮询接口返回）。
type DocumentProgress struct {
	DocumentID int64  `json:"documentId"`
	Status     string `json:"status"`
	Progress   int16  `json:"progress"`
	Stage      string `json:"stage"` // 当前阶段中文描述，用于进度可视化
	ParseError string `json:"parseError,omitempty"`
}
