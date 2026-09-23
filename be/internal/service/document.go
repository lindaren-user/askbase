package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"askbase/be/internal/filetext"
	"askbase/be/internal/model"
	"askbase/be/internal/repo"
	"askbase/be/internal/storage"
	"askbase/be/internal/tx"
	"askbase/be/internal/worker"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type documentStore interface {
	Create(ctx context.Context, doc model.Document) (model.Document, error)
	GetByID(ctx context.Context, id int64) (model.Document, error)
	ListByDataset(ctx context.Context, datasetID int64) ([]model.Document, error)
	Update(ctx context.Context, id int64, name *string, enabled *bool) error
	UpdateStatus(ctx context.Context, id int64, status string, progress int16, parseError string) error
	ResetForParse(ctx context.Context, id int64, status string, progress int16) (model.Document, error)
	UpdateEnabledByIDs(ctx context.Context, ids []int64, enabled bool) error
	Delete(ctx context.Context, id int64) error
}

type documentOutboxStore interface {
	Create(ctx context.Context, event model.Outbox) (model.Outbox, error)
}

type chunkByDocument interface {
	DeleteByDocument(ctx context.Context, documentID int64) error
	ListImageKeysByDocument(ctx context.Context, documentID int64) ([]string, error)
}

type documentObjectStore interface {
	PresignPut(ctx context.Context, key string, contentType string, expires time.Duration) (storage.UploadToken, error)
	PresignGet(ctx context.Context, key string, filename string, contentType string, expires time.Duration) (string, error)
	Get(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
}

// DocumentService 文档业务：预签名直传、登记、状态机、删除级联。
type DocumentService struct {
	db        *gorm.DB
	datasets  datasetByUserStore
	documents documentStore
	chunks    chunkByDocument
	objects   documentObjectStore
	prefix    string
	outbox    documentOutboxStore
}

// NewDocumentService 创建文档服务。
func NewDocumentService(
	db *gorm.DB,
	datasets datasetByUserStore,
	documents documentStore,
	chunks chunkByDocument,
	objects documentObjectStore,
	storagePrefix string,
	outbox documentOutboxStore,
) *DocumentService {
	return &DocumentService{
		db:        db,
		datasets:  datasets,
		documents: documents,
		chunks:    chunks,
		objects:   objects,
		prefix:    strings.Trim(strings.TrimSpace(storagePrefix), "/"),
		outbox:    outbox,
	}
}

// CreateUploadURL 校验知识库归属后生成 R2 预签名直传地址。
func (s *DocumentService) CreateUploadURL(ctx context.Context, userID int64, req model.UploadURLRequest) (storage.UploadToken, error) {
	if _, err := s.ownedDataset(ctx, userID, req.DatasetID); err != nil {
		return storage.UploadToken{}, err
	}
	filename := strings.TrimSpace(req.Filename)
	if filename == "" {
		return storage.UploadToken{}, fmt.Errorf("%w: 文件名不能为空", ErrInvalidInput)
	}
	if req.Size <= 0 {
		return storage.UploadToken{}, fmt.Errorf("%w: 文件大小不合法", ErrInvalidInput)
	}
	if req.Size > 100<<20 {
		return storage.UploadToken{}, fmt.Errorf("%w: 文件超过 100MB 限制", ErrInvalidInput)
	}
	ext := strings.ToLower(path.Ext(filename))
	key := fmt.Sprintf("%s/%d/%d/%s%s", s.prefix, userID, req.DatasetID, uuid.NewString(), ext)
	token, err := s.objects.PresignPut(ctx, key, req.ContentType, 10*time.Minute)
	if err != nil {
		return storage.UploadToken{}, fmt.Errorf("生成上传凭证失败: %w", err)
	}
	return token, nil
}

// Register 直传完成后登记文档；同知识库相同 hash 直接返回已有文档（幂等），并投递解析任务。
func (s *DocumentService) Register(ctx context.Context, userID int64, datasetID int64, req model.RegisterDocumentRequest) (model.Document, bool, error) {
	if _, err := s.ownedDataset(ctx, userID, datasetID); err != nil {
		return model.Document{}, false, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return model.Document{}, false, fmt.Errorf("%w: 文档名不能为空", ErrInvalidInput)
	}
	if strings.TrimSpace(req.R2Key) == "" {
		return model.Document{}, false, fmt.Errorf("%w: 缺少对象存储键", ErrInvalidInput)
	}
	var doc model.Document
	created := false
	err := tx.With(ctx, s.db, func(ctx context.Context) error {
		var err error
		doc, err = s.documents.Create(ctx, model.Document{
			DatasetID:    datasetID,
			Name:         name,
			R2Key:        req.R2Key,
			FileHash:     strings.TrimSpace(req.FileHash),
			SizeBytes:    req.SizeBytes,
			Status:       model.DocumentStatusPending,
			ParseVersion: 1,
			Enabled:      true,
		})
		if errors.Is(err, repo.ErrDocumentExists) {
			return nil
		}
		if err != nil {
			return err
		}
		created = true
		return s.createParseOutbox(ctx, doc)
	})
	if err != nil {
		return model.Document{}, false, err
	}
	return doc, created, nil
}

// Get 查询单个文档；校验归属。
func (s *DocumentService) Get(ctx context.Context, userID int64, id int64) (model.Document, error) {
	return s.ownedDocument(ctx, userID, id)
}

// List 列出知识库文档。
func (s *DocumentService) List(ctx context.Context, userID int64, datasetID int64) ([]model.Document, error) {
	if _, err := s.ownedDataset(ctx, userID, datasetID); err != nil {
		return nil, err
	}
	return s.documents.ListByDataset(ctx, datasetID)
}

// Update 重命名或启停文档；停用后其分块不参与检索。
func (s *DocumentService) Update(ctx context.Context, userID int64, id int64, req model.UpdateDocumentRequest) error {
	if _, err := s.ownedDocument(ctx, userID, id); err != nil {
		return err
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return fmt.Errorf("%w: 文档名不能为空", ErrInvalidInput)
		}
		req.Name = &name
	}
	err := s.documents.Update(ctx, id, req.Name, req.Enabled)
	return asNotFound(err, repo.ErrDocumentNotFound)
}

func pipelineBusy(status string) bool {
	switch status {
	case model.DocumentStatusPending, model.DocumentStatusParsing, model.DocumentStatusChunking, model.DocumentStatusEmbedding:
		return true
	default:
		return false
	}
}

// Stop 请求停止解析：置 cancelling，worker 在阶段边界退出。
func (s *DocumentService) Stop(ctx context.Context, userID int64, id int64) error {
	doc, err := s.ownedDocument(ctx, userID, id)
	if err != nil {
		return err
	}
	if !pipelineBusy(doc.Status) {
		return fmt.Errorf("%w: 当前状态不可停止", ErrConflict)
	}
	status := model.DocumentStatusCancelling
	parseError := ""
	if doc.Status == model.DocumentStatusPending {
		status = model.DocumentStatusCancelled
		parseError = "已停止"
	}
	err = s.documents.UpdateStatus(ctx, id, status, doc.Progress, parseError)
	return asNotFound(err, repo.ErrDocumentNotFound)
}

// BatchStatus 批量启停文档；仅处理当前用户知识库内的 ID。
func (s *DocumentService) BatchStatus(ctx context.Context, userID int64, req model.BatchDocumentStatusRequest) error {
	if len(req.IDs) == 0 {
		return fmt.Errorf("%w: 请选择文档", ErrInvalidInput)
	}
	owned := make([]int64, 0, len(req.IDs))
	for _, id := range req.IDs {
		if _, err := s.ownedDocument(ctx, userID, id); err != nil {
			return err
		}
		owned = append(owned, id)
	}
	return s.documents.UpdateEnabledByIDs(ctx, owned, req.Enabled)
}

// Preview 在线预览：txt/csv 返回纯文本，md 返回 markdown，pdf/图片返回预签名 URL。
func (s *DocumentService) Preview(ctx context.Context, userID int64, id int64) (model.DocumentPreview, error) {
	doc, err := s.storedDocument(ctx, userID, id)
	if err != nil {
		return model.DocumentPreview{}, err
	}
	ext := strings.ToLower(path.Ext(doc.Name))
	if ext == ".pdf" {
		return s.previewPDF(ctx, doc)
	}
	if isImageExt(ext) {
		url, err := s.objects.PresignGet(ctx, doc.R2Key, "", imageContentType(doc.Name), 10*time.Minute)
		if err != nil {
			return model.DocumentPreview{}, fmt.Errorf("生成预览地址失败: %w", err)
		}
		return model.DocumentPreview{Kind: "image", URL: url, Name: doc.Name}, nil
	}
	data, err := s.objects.Get(ctx, doc.R2Key)
	if err != nil {
		return model.DocumentPreview{}, fmt.Errorf("读取文档失败: %w", err)
	}
	if filetext.IsPDF(data) {
		return s.previewPDF(ctx, doc)
	}
	switch ext {
	case ".txt", ".csv", ".md", ".markdown":
		text := strings.ReplaceAll(filetext.Decode(data), "\r\n", "\n")
		return textPreview(doc.Name, text), nil
	case ".docx":
		return extractedPreview(doc.Name, data, worker.ExtractDocxPreview)
	case ".xlsx":
		return extractedPreview(doc.Name, data, worker.ExtractXlsxText)
	default:
		return model.DocumentPreview{}, fmt.Errorf("%w: 不支持预览该文件类型", ErrInvalidInput)
	}
}

func extractedPreview(name string, data []byte, extract func([]byte) (string, error)) (model.DocumentPreview, error) {
	text, err := extract(data)
	if err != nil {
		return model.DocumentPreview{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	return textPreview(name, text), nil
}

func textPreview(name, text string) model.DocumentPreview {
	const maxPreview = 512 << 10
	if len(text) > maxPreview {
		text = text[:maxPreview] + "\n\n…（预览已截断）"
	}
	kind := "text"
	switch strings.ToLower(path.Ext(name)) {
	case ".md", ".markdown", ".docx", ".xlsx":
		kind = "markdown"
	}
	return model.DocumentPreview{Kind: kind, Content: text, Name: name}
}

func (s *DocumentService) previewPDF(ctx context.Context, doc model.Document) (model.DocumentPreview, error) {
	url, err := s.objects.PresignGet(ctx, doc.R2Key, "", "application/pdf", 10*time.Minute)
	if err != nil {
		return model.DocumentPreview{}, fmt.Errorf("生成预览地址失败: %w", err)
	}
	return model.DocumentPreview{Kind: "pdf", URL: url, Name: doc.Name}, nil
}

func (s *DocumentService) Download(ctx context.Context, userID int64, id int64) (model.DocumentDownload, error) {
	doc, err := s.storedDocument(ctx, userID, id)
	if err != nil {
		return model.DocumentDownload{}, err
	}
	url, err := s.objects.PresignGet(ctx, doc.R2Key, doc.Name, previewContentType(doc.Name), 10*time.Minute)
	if err != nil {
		return model.DocumentDownload{}, fmt.Errorf("生成下载地址失败: %w", err)
	}
	return model.DocumentDownload{URL: url, Filename: doc.Name}, nil
}

func (s *DocumentService) storedDocument(ctx context.Context, userID int64, id int64) (model.Document, error) {
	doc, err := s.ownedDocument(ctx, userID, id)
	if err != nil {
		return model.Document{}, err
	}
	if strings.TrimSpace(doc.R2Key) == "" {
		return model.Document{}, fmt.Errorf("%w: 文档文件不存在", ErrInvalidInput)
	}
	return doc, nil
}

func previewContentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".pdf":
		return "application/pdf"
	case ".md", ".markdown":
		return "text/markdown; charset=utf-8"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".csv":
		return "text/csv; charset=utf-8"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	default:
		return ""
	}
}

// Delete 删除文档：先清分块与对象，再删行。
func (s *DocumentService) Delete(ctx context.Context, userID int64, id int64) error {
	doc, err := s.ownedDocument(ctx, userID, id)
	if err != nil {
		return err
	}
	if keys, err := s.chunks.ListImageKeysByDocument(ctx, id); err == nil {
		deleteObjectKeys(ctx, s.objects, keys)
	}
	if err := s.chunks.DeleteByDocument(ctx, id); err != nil {
		return fmt.Errorf("删除文档分块失败: %w", err)
	}
	if doc.R2Key != "" {
		if err := s.objects.Delete(ctx, doc.R2Key); err != nil {
			// 对象删除失败不阻塞主流程，记录日志由运维兜底
			zap.L().Warn("删除对象存储文件失败", zap.String("key", doc.R2Key), zap.Error(err))
		}
	}
	err = s.documents.Delete(ctx, id)
	return asNotFound(err, repo.ErrDocumentNotFound)
}

// TriggerParse 触发/重试解析：重置状态机为 pending 并重新投递队列；embed 为自定义嵌入模型覆盖。
func (s *DocumentService) TriggerParse(ctx context.Context, userID int64, id int64) error {
	doc, err := s.ownedDocument(ctx, userID, id)
	if err != nil {
		return err
	}
	// 解析中不允许重复触发，避免并发消费同一文档
	switch doc.Status {
	case model.DocumentStatusParsing, model.DocumentStatusChunking, model.DocumentStatusEmbedding:
		return fmt.Errorf("%w: 文档正在解析中", ErrConflict)
	}
	return tx.With(ctx, s.db, func(ctx context.Context) error {
		reset, err := s.documents.ResetForParse(ctx, id, model.DocumentStatusPending, 0)
		if err != nil {
			return asNotFound(err, repo.ErrDocumentNotFound)
		}
		return s.createParseOutbox(ctx, reset)
	})
}

// Progress 查询文档流水线进度，供前端轮询与进度可视化。
func (s *DocumentService) Progress(ctx context.Context, userID int64, id int64) (model.DocumentProgress, error) {
	doc, err := s.ownedDocument(ctx, userID, id)
	if err != nil {
		return model.DocumentProgress{}, err
	}
	progress := model.DocumentProgress{
		DocumentID: doc.ID,
		Status:     doc.Status,
		Progress:   doc.Progress,
		Stage:      stageText(doc.Status),
	}
	if doc.ParseError != nil {
		progress.ParseError = *doc.ParseError
	}
	return progress, nil
}

// stageText 状态机各阶段的中文描述。
func stageText(status string) string {
	switch status {
	case model.DocumentStatusPending:
		return "排队等待"
	case model.DocumentStatusParsing:
		return "解析文本"
	case model.DocumentStatusChunking:
		return "切分分块"
	case model.DocumentStatusEmbedding:
		return "向量化"
	case model.DocumentStatusDone:
		return "已完成"
	case model.DocumentStatusFailed:
		return "失败"
	case model.DocumentStatusCancelling:
		return "正在停止"
	case model.DocumentStatusCancelled:
		return "已停止"
	default:
		return status
	}
}

// createParseOutbox 在当前事务中登记文档解析事件。
func (s *DocumentService) createParseOutbox(ctx context.Context, doc model.Document) error {
	payload, err := json.Marshal(model.DocumentParseEvent{
		DocumentID:   doc.ID,
		ParseVersion: doc.ParseVersion,
		Attempt:      1,
	})
	if err != nil {
		return fmt.Errorf("编码文档解析事件失败: %w", err)
	}
	_, err = s.outbox.Create(ctx, model.Outbox{
		Topic:         model.OutboxTopicDocumentParse,
		AggregateID:   doc.ID,
		Payload:       payload,
		Status:        model.OutboxStatusPending,
		NextAttemptAt: time.Now(),
	})
	return err
}

// ownedDataset 校验知识库归属并返回。
func (s *DocumentService) ownedDataset(ctx context.Context, userID int64, datasetID int64) (model.Dataset, error) {
	ds, err := s.datasets.GetByUser(ctx, userID, datasetID)
	if err != nil {
		return model.Dataset{}, asNotFound(err, repo.ErrDatasetNotFound)
	}
	return ds, nil
}

// ownedDocument 校验文档归属（经知识库 join 用户）并返回。
func (s *DocumentService) ownedDocument(ctx context.Context, userID int64, id int64) (model.Document, error) {
	doc, err := s.documents.GetByID(ctx, id)
	if err != nil {
		return model.Document{}, asNotFound(err, repo.ErrDocumentNotFound)
	}
	if _, err := s.ownedDataset(ctx, userID, doc.DatasetID); err != nil {
		return model.Document{}, err
	}
	return doc, nil
}

func isImageExt(ext string) bool {
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp":
		return true
	default:
		return false
	}
}
