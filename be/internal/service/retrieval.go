package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"askbase/be/internal/config"
	"askbase/be/internal/model"
	"askbase/be/internal/repo"

	"go.uber.org/zap"
)

type retrievalStore interface {
	SearchByVector(ctx context.Context, datasetID int64, values []float32, topK int, minScore float64) ([]model.RetrievalHit, error)
	SearchByFullText(ctx context.Context, datasetID int64, query string, topK int) ([]model.RetrievalHit, error)
}

type textEmbedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type datasetEmbedderLoader func(ctx context.Context, ds model.Dataset) (textEmbedder, error)

const rrfK = 60.0

type retrievalRoutes struct {
	vectorHits   []model.RetrievalHit
	fullTextHits []model.RetrievalHit
	fullTextErr  error
}

// RetrievalService 检索服务：对每条查询执行向量和全文混合检索，再合并多查询结果。
type RetrievalService struct {
	datasets      datasetByUserStore
	embedModels   embedModelReader
	chunks        retrievalStore
	objects       objectURLSigner
	topK          int
	minScore      float64
	embedOfficial config.EmbeddingConfig
	loadEmbedder  datasetEmbedderLoader
}

// NewRetrievalService 创建检索服务。
func NewRetrievalService(
	datasets datasetByUserStore,
	embedModels embedModelReader,
	chunks retrievalStore,
	objects objectURLSigner,
	cfg config.ChatRetrievalConfig,
	embedOfficial config.EmbeddingConfig,
) *RetrievalService {
	service := &RetrievalService{
		datasets:      datasets,
		embedModels:   embedModels,
		chunks:        chunks,
		objects:       objects,
		topK:          cfg.TopK,
		minScore:      cfg.MinScore,
		embedOfficial: embedOfficial,
	}
	service.loadEmbedder = func(ctx context.Context, ds model.Dataset) (textEmbedder, error) {
		return loadDatasetEmbedder(ctx, service.embedModels, ds, service.embedOfficial)
	}
	return service
}

// Search 在知识库内执行单查询检索；嵌入模型始终采用知识库绑定配置。
func (s *RetrievalService) Search(
	ctx context.Context,
	userID int64,
	datasetID int64,
	query string,
	topK int,
	minScore float64,
) ([]model.RetrievalHit, error) {
	return s.SearchQueries(ctx, userID, datasetID, []string{query}, topK, minScore)
}

// SearchQueries 对多条查询分别执行混合检索，再通过二级 RRF 合并并去重。
func (s *RetrievalService) SearchQueries(
	ctx context.Context,
	userID int64,
	datasetID int64,
	queries []string,
	topK int,
	minScore float64,
) ([]model.RetrievalHit, error) {
	// 1. 清洗并去重查询，同时校验知识库归属和检索参数。
	queries = normalizeRetrievalQueries(queries)
	if len(queries) == 0 {
		return nil, fmt.Errorf("%w: 查询内容不能为空", ErrInvalidInput)
	}
	ds, err := s.datasets.GetByUser(ctx, userID, datasetID)
	if err != nil {
		return nil, asNotFound(err, repo.ErrDatasetNotFound)
	}
	if topK <= 0 {
		topK = s.topK
	}
	if topK > 50 {
		topK = 50
	}
	if minScore <= 0 {
		minScore = s.minScore
	}

	// 2. 使用知识库绑定的嵌入模型批量生成查询向量，确保查询与文档向量空间一致。
	emb, err := s.loadEmbedder(ctx, ds)
	if err != nil {
		return nil, err
	}
	vectors, err := emb.Embed(ctx, queries)
	if err != nil {
		return nil, fmt.Errorf("查询向量化失败: %w", err)
	}
	if len(vectors) != len(queries) {
		return nil, fmt.Errorf("查询向量化失败: 返回向量数量不匹配")
	}

	// 3. 每条查询并行执行向量召回和全文召回，并用一级 RRF 融合两路排名。
	// 全文检索异常时仅降级当前查询，保留已经成功的向量召回结果。
	queryResults := make([][]model.RetrievalHit, 0, len(queries))
	for i, query := range queries {
		if len(vectors[i]) == 0 {
			return nil, fmt.Errorf("查询向量化失败: 第 %d 条查询返回空向量", i+1)
		}
		routes, err := s.searchRetrievalRoutes(
			ctx,
			datasetID,
			query,
			vectors[i],
			topK,
			minScore,
		)
		if err != nil {
			return nil, fmt.Errorf("第 %d 条查询的向量检索失败: %w", i+1, err)
		}
		if routes.fullTextErr != nil {
			zap.L().Warn(
				"全文检索失败，当前查询降级为向量检索",
				zap.Int("queryIndex", i),
				zap.String("query", query),
				zap.Error(routes.fullTextErr),
			)
			queryResults = append(queryResults, routes.vectorHits)
			continue
		}
		queryResults = append(queryResults, rrfMerge(topK, routes.vectorHits, routes.fullTextHits))
	}

	// 4. 使用二级 RRF 融合多条查询结果，按 chunk 去重后补充图片访问地址。
	merged := rrfMerge(topK, queryResults...)
	attachHitImageURLs(ctx, s.objects, merged)
	return merged, nil
}

// searchRetrievalRoutes 并行执行单条查询的向量与全文召回。
// 向量召回失败时取消全文召回；全文召回失败由调用方决定是否降级。
func (s *RetrievalService) searchRetrievalRoutes(
	ctx context.Context,
	datasetID int64,
	query string,
	vector []float32,
	topK int,
	minScore float64,
) (retrievalRoutes, error) {
	searchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var routes retrievalRoutes
	var vectorErr error
	var waitGroup sync.WaitGroup
	waitGroup.Add(2)

	go func() {
		defer waitGroup.Done()
		routes.vectorHits, vectorErr = s.chunks.SearchByVector(searchCtx, datasetID, vector, topK, minScore)
		if vectorErr != nil {
			cancel()
		}
	}()

	go func() {
		defer waitGroup.Done()
		routes.fullTextHits, routes.fullTextErr = s.chunks.SearchByFullText(searchCtx, datasetID, query, topK)
	}()

	waitGroup.Wait()
	return routes, vectorErr
}

// normalizeRetrievalQueries 清理空查询，并按不区分大小写的文本值稳定去重。
func normalizeRetrievalQueries(queries []string) []string {
	result := make([]string, 0, len(queries))
	seen := make(map[string]struct{}, len(queries))
	for _, query := range queries {
		query = strings.TrimSpace(query)
		if query == "" {
			continue
		}
		key := strings.ToLower(query)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, query)
	}
	return result
}

// rrfMerge 使用 Reciprocal Rank Fusion 融合多个有序召回列表。
// 同一分块累加各列表中的排名分数，同时保留其最高原始相关度和图片信息。
func rrfMerge(topK int, lists ...[]model.RetrievalHit) []model.RetrievalHit {
	type entry struct {
		hit model.RetrievalHit
		rrf float64
	}
	acc := make(map[int64]*entry)
	for _, list := range lists {
		for rank, hit := range list {
			e, ok := acc[hit.ChunkID]
			if !ok {
				e = &entry{hit: hit}
				acc[hit.ChunkID] = e
			}
			e.rrf += 1 / (rrfK + float64(rank) + 1)
			if hit.Score > e.hit.Score {
				e.hit.Score = hit.Score
			}
			if e.hit.ImageKey == "" {
				e.hit.ImageKey = hit.ImageKey
			}
		}
	}
	entries := make([]entry, 0, len(acc))
	for _, e := range acc {
		entries = append(entries, *e)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].rrf == entries[j].rrf {
			return entries[i].hit.ChunkID < entries[j].hit.ChunkID
		}
		return entries[i].rrf > entries[j].rrf
	})
	if topK > 0 && len(entries) > topK {
		entries = entries[:topK]
	}
	merged := make([]model.RetrievalHit, 0, len(entries))
	for _, e := range entries {
		merged = append(merged, e.hit)
	}
	return merged
}
