package service

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"askbase/be/internal/config"
	"askbase/be/internal/model"
	"askbase/be/internal/repo"
)

type chunkStore interface {
	ListByDocument(ctx context.Context, documentID int64) ([]model.Chunk, error)
	GetByID(ctx context.Context, id int64) (model.Chunk, error)
	Update(ctx context.Context, id int64, enabled *bool, content *string, tokenNum *int) error
	UpdateVector(ctx context.Context, id int64, values []float32) error
}

type chunkDocumentReader interface {
	GetByID(ctx context.Context, id int64) (model.Document, error)
}

// ChunkService 分块业务。
type ChunkService struct {
	chunks        chunkStore
	documents     chunkDocumentReader
	datasets      datasetByUserStore
	embedModels   embedModelReader
	objects       objectURLSigner
	embedOfficial config.EmbeddingConfig
}

// NewChunkService 创建分块服务。
func NewChunkService(chunks chunkStore, documents chunkDocumentReader, datasets datasetByUserStore, embedModels embedModelReader, objects objectURLSigner, embedOfficial config.EmbeddingConfig) *ChunkService {
	return &ChunkService{chunks: chunks, documents: documents, datasets: datasets, embedModels: embedModels, objects: objects, embedOfficial: embedOfficial}
}

// ListByDocument 列出文档分块；校验文档归属。
func (s *ChunkService) ListByDocument(ctx context.Context, userID int64, documentID int64) ([]model.Chunk, error) {
	if err := s.checkDocumentOwner(ctx, userID, documentID); err != nil {
		return nil, err
	}
	items, err := s.chunks.ListByDocument(ctx, documentID)
	if err != nil {
		return nil, err
	}
	attachChunkImageURLs(ctx, s.objects, items)
	return items, nil
}

// Update 更新分块启停或内容；改内容时同步重算向量，不整篇重跑。
func (s *ChunkService) Update(ctx context.Context, userID int64, id int64, req model.UpdateChunkRequest) error {
	chunk, err := s.chunks.GetByID(ctx, id)
	if err != nil {
		return asNotFound(err, repo.ErrChunkNotFound)
	}
	if err := s.checkDocumentOwner(ctx, userID, chunk.DocumentID); err != nil {
		return err
	}
	var tokenNum *int
	if req.Content != nil {
		content := strings.TrimSpace(*req.Content)
		if content == "" {
			return fmt.Errorf("%w: 分块内容不能为空", ErrInvalidInput)
		}
		req.Content = &content
		n := utf8.RuneCountInString(content)/2 + 1
		tokenNum = &n
	}
	if err := s.chunks.Update(ctx, id, req.Enabled, req.Content, tokenNum); err != nil {
		return asNotFound(err, repo.ErrChunkNotFound)
	}
	if req.Content == nil {
		return nil
	}
	doc, err := s.documents.GetByID(ctx, chunk.DocumentID)
	if err != nil {
		return asNotFound(err, repo.ErrDocumentNotFound)
	}
	ds, err := s.datasets.GetByUser(ctx, userID, doc.DatasetID)
	if err != nil {
		return asNotFound(err, repo.ErrDatasetNotFound)
	}
	emb, err := loadDatasetEmbedder(ctx, s.embedModels, ds, s.embedOfficial)
	if err != nil {
		return err
	}
	vectors, err := emb.Embed(ctx, []string{model.EmbedText(doc.Name, *req.Content)})
	if err != nil {
		return fmt.Errorf("重算分块向量失败: %w", err)
	}
	if len(vectors) == 0 {
		return fmt.Errorf("重算分块向量失败: 返回为空")
	}
	return s.chunks.UpdateVector(ctx, id, vectors[0])
}

// checkDocumentOwner 校验文档属于当前用户。
func (s *ChunkService) checkDocumentOwner(ctx context.Context, userID int64, documentID int64) error {
	doc, err := s.documents.GetByID(ctx, documentID)
	if err != nil {
		return asNotFound(err, repo.ErrDocumentNotFound)
	}
	if _, err := s.datasets.GetByUser(ctx, userID, doc.DatasetID); err != nil {
		return asNotFound(err, repo.ErrDatasetNotFound)
	}
	return nil
}
