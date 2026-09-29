package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"askbase/be/internal/config"
	"askbase/be/internal/model"
	"askbase/be/internal/service"
	"askbase/be/internal/worker/chunker"

	"gorm.io/gorm"
)

type queryRow struct {
	ID      string          `json:"id"`
	Text    string          `json:"text"`
	History []model.Message `json:"history,omitempty"`
}

type rankedHit struct {
	DocumentID string  `json:"documentId"`
	ChunkID    string  `json:"chunkId"`
	Rank       int     `json:"rank"`
	Score      float64 `json:"score"`
}

type collectedQuery struct {
	ID         string            `json:"id"`
	Text       string            `json:"text"`
	Plan       service.QueryPlan `json:"plan"`
	RouteError string            `json:"routeError,omitempty"`
	DurationMS float64           `json:"durationMs"`
	Hits       []rankedHit       `json:"hits"`
}

type collection struct {
	GeneratedAt    time.Time        `json:"generatedAt"`
	DatasetID      int64            `json:"datasetId"`
	Mode           string           `json:"mode"`
	TopK           int              `json:"topK"`
	MinScore       float64          `json:"minScore"`
	SourceHash     string           `json:"sourceHash"`
	DocumentCount  int              `json:"documentCount"`
	ChunkTarget    int              `json:"chunkTargetRunes"`
	ChunkOverlap   int              `json:"chunkOverlapRunes"`
	EmbeddingModel string           `json:"embeddingModel"`
	RouterModel    string           `json:"routerModel,omitempty"`
	Results        []collectedQuery `json:"results"`
}

type queryRetriever interface {
	SearchQueries(context.Context, int64, int64, []string, int, float64) ([]model.RetrievalHit, error)
}

// collect 读取查询文件，逐题调用产品检索服务，并将排名及运行配置写入 JSON。
func collect(ctx context.Context, db *gorm.DB, dataset model.Dataset, retrieval queryRetriever,
	planner service.QueryPlanner, input, output, mode string, topK int, minScore float64, cfg config.Config) error {
	idByDocument, sourceHash, err := benchmarkDocumentIDs(ctx, db, dataset.ID)
	if err != nil {
		return err
	}
	file, err := os.Open(input)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	result := collection{GeneratedAt: time.Now().UTC(), DatasetID: dataset.ID,
		Mode: mode, TopK: topK, MinScore: minScore, SourceHash: sourceHash, EmbeddingModel: dataset.EmbedModel,
		DocumentCount: len(idByDocument), ChunkTarget: chunker.DefaultTargetRunes,
		ChunkOverlap: chunker.DefaultOverlapRunes,
		Results:      []collectedQuery{}}
	if mode == "planned" {
		result.RouterModel = cfg.LLM.Model
	}
	seen := map[string]struct{}{}
	line := 0
	for scanner.Scan() {
		line++
		var query queryRow
		if err := json.Unmarshal(scanner.Bytes(), &query); err != nil {
			return fmt.Errorf("queries line %d: %w", line, err)
		}
		query.ID = strings.TrimSpace(query.ID)
		query.Text = strings.TrimSpace(query.Text)
		if query.ID == "" || query.Text == "" {
			return fmt.Errorf("queries line %d: id/text missing", line)
		}
		if _, exists := seen[query.ID]; exists {
			return fmt.Errorf("queries line %d: duplicate id %q", line, query.ID)
		}
		seen[query.ID] = struct{}{}
		item, err := collectOne(ctx, dataset, retrieval, planner, idByDocument, query, mode, topK, minScore)
		if err != nil {
			return fmt.Errorf("query %s: %w", query.ID, err)
		}
		result.Results = append(result.Results, item)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if len(result.Results) == 0 {
		return fmt.Errorf("queries file is empty")
	}
	if err := writeJSONAtomic(output, result); err != nil {
		return err
	}
	fmt.Printf("collected %d queries into %s\n", len(result.Results), output)
	return nil
}

// collectOne 按指定模式检索单个问题，并将数据库文档 ID 映射回公开语料 ID。
// planned 模式保留查询规划及错误信息；无需检索的规划结果返回空命中列表。
func collectOne(ctx context.Context, dataset model.Dataset, retrieval queryRetriever, planner service.QueryPlanner,
	idByDocument map[int64]string, query queryRow, mode string, topK int, minScore float64) (collectedQuery, error) {
	start := time.Now()
	result := collectedQuery{ID: query.ID, Text: query.Text, Hits: []rankedHit{}}
	result.Plan = service.QueryPlan{QueryType: service.QueryTypeDirect, Queries: []string{query.Text}}
	if mode == "planned" {
		if planner == nil {
			return result, fmt.Errorf("planned mode requires a query planner")
		}
		plan, err := planner.Plan(ctx, service.QueryRouteInput{Question: query.Text, History: query.History})
		result.Plan = plan
		if err != nil {
			result.RouteError = err.Error() // 线上查询规划器会返回直接检索的兜底方案。
		}
	}
	if result.Plan.QueryType != service.QueryTypeNone {
		hits, err := retrieval.SearchQueries(ctx, dataset.UserID, dataset.ID, result.Plan.Queries, topK, minScore)
		if err != nil {
			return result, err
		}
		for rank, hit := range hits {
			id, exists := idByDocument[hit.DocumentID]
			if !exists {
				return result, fmt.Errorf("retrieved document %d is not in benchmark corpus", hit.DocumentID)
			}
			result.Hits = append(result.Hits, rankedHit{DocumentID: id,
				ChunkID: fmt.Sprint(hit.ChunkID), Rank: rank + 1, Score: hit.Score})
		}
	}
	result.DurationMS = float64(time.Since(start).Microseconds()) / 1000
	return result, nil
}

// benchmarkDocumentIDs 校验知识库仅包含已完成的基准文档，并返回文档 ID 映射和语料指纹。
func benchmarkDocumentIDs(ctx context.Context, db *gorm.DB, datasetID int64) (map[int64]string, string, error) {
	var documents []model.Document
	if err := db.WithContext(ctx).Select("id", "r2_key", "status", "file_hash").
		Where("dataset_id = ?", datasetID).Find(&documents).Error; err != nil {
		return nil, "", err
	}
	if len(documents) == 0 {
		return nil, "", fmt.Errorf("benchmark dataset %d is empty; run index first", datasetID)
	}
	ids := make(map[int64]string, len(documents))
	keys := make([]string, 0, len(documents))
	for _, document := range documents {
		if !strings.HasPrefix(document.R2Key, benchmarkKeyPrefix) || document.Status != model.DocumentStatusDone {
			return nil, "", fmt.Errorf("dataset %d contains a non-benchmark or incomplete document %d", datasetID, document.ID)
		}
		id := strings.TrimPrefix(document.R2Key, benchmarkKeyPrefix)
		ids[document.ID] = id
		keys = append(keys, id+"\t"+document.FileHash+"\n")
	}
	sort.Strings(keys)
	hash := sha256.New()
	for _, key := range keys {
		_, _ = hash.Write([]byte(key))
	}
	return ids, fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// writeJSONAtomic 先写入同目录临时文件，再重命名为目标文件，避免留下不完整结果。
func writeJSONAtomic(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".retrieval-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	encoder := json.NewEncoder(temp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}
