package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"askbase/be/internal/model"
	"askbase/be/internal/repo"
	"askbase/be/internal/worker/chunker"

	"gorm.io/gorm"
)

const benchmarkKeyPrefix = "public-benchmark:"

type corpusRow struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

type embedClient interface {
	Embed(context.Context, []string) ([][]float32, error)
}

type pendingDocument struct {
	document model.Document
	chunks   []model.Chunk
}

// indexCorpus 将每条基准语料作为文档写入，并用通用文本切分器建立检索索引。
// 原始文档 ID 保存在 r2_key 中，采集结果可据此映射回公开数据集的相关性标注。
func indexCorpus(ctx context.Context, db *gorm.DB, chunks *repo.ChunkRepository, dataset model.Dataset, embedder embedClient, path string, rebuild bool) error {
	var foreign int64
	if err := db.WithContext(ctx).Model(&model.Document{}).
		Where("dataset_id = ? AND r2_key NOT LIKE ?", dataset.ID, benchmarkKeyPrefix+"%").Count(&foreign).Error; err != nil {
		return err
	}
	if foreign > 0 {
		return fmt.Errorf("dataset %d contains non-benchmark documents; use a dedicated dataset", dataset.ID)
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	seen := make(map[string]struct{})
	pending := make([]pendingDocument, 0, 32)
	pendingChunks := 0
	line := 0
	indexed := 0
	for scanner.Scan() {
		line++
		var row corpusRow
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return fmt.Errorf("corpus line %d: %w", line, err)
		}
		row.ID = strings.TrimSpace(row.ID)
		row.Text = strings.TrimSpace(row.Text)
		if row.ID == "" || row.Text == "" || len(row.ID) > 900 {
			return fmt.Errorf("corpus line %d: id/text missing or id too long", line)
		}
		if _, exists := seen[row.ID]; exists {
			return fmt.Errorf("corpus line %d: duplicate id %q", line, row.ID)
		}
		seen[row.ID] = struct{}{}
		key := benchmarkKeyPrefix + row.ID
		name := strings.TrimSpace(row.Title)
		if name == "" {
			name = row.ID
		}
		if len([]rune(name)) > 200 {
			name = string([]rune(name)[:200])
		}
		sum := sha256.Sum256([]byte(key + "\n" + name + "\n" + row.Text))
		hash := hex.EncodeToString(sum[:])
		var document model.Document
		err := db.WithContext(ctx).Where("dataset_id = ? AND r2_key = ?", dataset.ID, key).Take(&document).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil && document.Status == model.DocumentStatusDone && document.FileHash == hash && !rebuild {
			continue
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			document = model.Document{DatasetID: dataset.ID, Name: name, R2Key: key,
				FileHash: hash, SizeBytes: int64(len(row.Text)), Status: model.DocumentStatusEmbedding, Enabled: true}
			if err := db.WithContext(ctx).Create(&document).Error; err != nil {
				return err
			}
		} else {
			if err := chunks.DeleteByDocument(ctx, document.ID); err != nil {
				return err
			}
			if err := db.WithContext(ctx).Model(&document).Updates(map[string]any{
				"name": name, "file_hash": hash, "size_bytes": len(row.Text),
				"status": model.DocumentStatusEmbedding, "enabled": true,
			}).Error; err != nil {
				return err
			}
		}
		// TODO: 当前绕过正式解析与分块流水线，也未读取知识库的分块策略。
		// 后续按 general、book、paper、resume、qa 分别选取匹配的公开原始文档，
		// 复用 Worker 的实际解析、分块及嵌入路径，再比较不同策略的检索表现。
		pieces := chunker.SplitRecursive(row.Text, chunker.DefaultTargetRunes, chunker.DefaultOverlapRunes)
		if len(pieces) == 0 {
			return fmt.Errorf("corpus line %d produced no chunks", line)
		}
		if pendingChunks+len(pieces) > 32 && len(pending) > 0 {
			if err := embedPending(ctx, db, chunks, embedder, pending); err != nil {
				return err
			}
			indexed += len(pending)
			pending = pending[:0]
			pendingChunks = 0
		}
		documentChunks := make([]model.Chunk, 0, len(pieces))
		for index, piece := range pieces {
			documentChunks = append(documentChunks, model.Chunk{
				ID:         chunker.IDFromContent(document.ID, index, piece),
				DocumentID: document.ID, DatasetID: dataset.ID, ChunkIndex: index,
				Content: piece, TokenNum: chunker.EstimateTokens(piece), Enabled: true,
				ElementType: "text", ParserName: "public-benchmark",
			})
		}
		if err := chunks.BulkInsert(ctx, documentChunks); err != nil {
			return err
		}
		pending = append(pending, pendingDocument{document: document, chunks: documentChunks})
		pendingChunks += len(documentChunks)
		if pendingChunks >= 32 {
			if err := embedPending(ctx, db, chunks, embedder, pending); err != nil {
				return err
			}
			indexed += len(pending)
			pending = pending[:0]
			pendingChunks = 0
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if len(seen) == 0 {
		return fmt.Errorf("corpus is empty")
	}
	if len(pending) > 0 {
		if err := embedPending(ctx, db, chunks, embedder, pending); err != nil {
			return err
		}
		indexed += len(pending)
	}
	var indexedDocuments []model.Document
	if err := db.WithContext(ctx).Select("r2_key", "status").Where("dataset_id = ?", dataset.ID).
		Find(&indexedDocuments).Error; err != nil {
		return err
	}
	if len(indexedDocuments) != len(seen) {
		return fmt.Errorf("dataset contains %d benchmark documents, corpus contains %d; use a fresh dataset", len(indexedDocuments), len(seen))
	}
	for _, document := range indexedDocuments {
		id := strings.TrimPrefix(document.R2Key, benchmarkKeyPrefix)
		if _, ok := seen[id]; !ok || document.Status != model.DocumentStatusDone {
			return fmt.Errorf("dataset contains stale or incomplete benchmark document %q", id)
		}
	}
	fmt.Printf("indexed %d passages (%d total) into dataset %d\n", indexed, len(seen), dataset.ID)
	return nil
}

// embedPending 批量生成待处理分块的向量；全部向量写入成功后才将对应文档标记为完成。
func embedPending(ctx context.Context, db *gorm.DB, chunks *repo.ChunkRepository, embedder embedClient, pending []pendingDocument) error {
	var texts []string
	var ids []int64
	for _, item := range pending {
		for _, chunk := range item.chunks {
			texts = append(texts, model.EmbedText(item.document.Name, chunk.Content))
			ids = append(ids, chunk.ID)
		}
	}
	for start := 0; start < len(texts); start += 32 {
		end := min(start+32, len(texts))
		vectors, err := embedder.Embed(ctx, texts[start:end])
		if err != nil {
			return err
		}
		if len(vectors) != end-start {
			return fmt.Errorf("embedding count mismatch: got %d, want %d", len(vectors), end-start)
		}
		for i, vector := range vectors {
			if len(vector) == 0 {
				return fmt.Errorf("empty embedding for chunk %d", ids[start+i])
			}
			if err := chunks.UpdateVector(ctx, ids[start+i], vector); err != nil {
				return err
			}
		}
	}
	for _, item := range pending {
		if err := db.WithContext(ctx).Model(&item.document).Updates(map[string]any{
			"status": model.DocumentStatusDone, "progress": 100,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}
