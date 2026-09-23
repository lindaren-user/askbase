package repo

import (
	"context"
	"fmt"
	"sync"

	"askbase/be/internal/model"

	"github.com/pgvector/pgvector-go"
)

const maxVectorDim = 4096

var (
	vecDDLMu   sync.Mutex
	vecEnsured sync.Map // int -> struct{}
)

// VectorColumn 返回 q_N_vec；N 必须在 1..4096。
func VectorColumn(dim int) (string, error) {
	if dim < 1 || dim > maxVectorDim {
		return "", fmt.Errorf("不支持的向量维度 %d", dim)
	}
	return fmt.Sprintf("q_%d_vec", dim), nil
}

// EnsureVectorColumn 若无 q_N_vec 则 ALTER 加列并建 HNSW。
func (r *ChunkRepository) EnsureVectorColumn(ctx context.Context, dim int) error {
	col, err := VectorColumn(dim)
	if err != nil {
		return err
	}
	if _, ok := vecEnsured.Load(dim); ok {
		return nil
	}
	vecDDLMu.Lock()
	defer vecDDLMu.Unlock()
	if _, ok := vecEnsured.Load(dim); ok {
		return nil
	}
	exists, err := r.vectorColumnExists(ctx, col)
	if err != nil {
		return err
	}
	if !exists {
		ddl := fmt.Sprintf("ALTER TABLE t_chunk ADD COLUMN %s vector(%d)", col, dim)
		if err := conn(ctx, r.db).Exec(ddl).Error; err != nil {
			return fmt.Errorf("添加向量列失败: %w", err)
		}
	}
	idx := fmt.Sprintf("idx_t_chunk_%s_hnsw", col)
	idxSQL := fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s ON t_chunk USING hnsw (%s vector_cosine_ops)",
		idx, col,
	)
	if err := conn(ctx, r.db).Exec(idxSQL).Error; err != nil {
		return fmt.Errorf("创建向量索引失败: %w", err)
	}
	vecEnsured.Store(dim, struct{}{})
	return nil
}

func (r *ChunkRepository) vectorColumnExists(ctx context.Context, col string) (bool, error) {
	var n int64
	err := conn(ctx, r.db).Raw(
		`SELECT COUNT(*) FROM information_schema.columns
		 WHERE table_schema = current_schema() AND table_name = 't_chunk' AND column_name = ?`,
		col,
	).Scan(&n).Error
	if err != nil {
		return false, fmt.Errorf("检查向量列失败: %w", err)
	}
	return n > 0, nil
}

// UpdateVector 按实测维度写入对应 q_N_vec。
func (r *ChunkRepository) UpdateVector(ctx context.Context, id int64, values []float32) error {
	if err := r.EnsureVectorColumn(ctx, len(values)); err != nil {
		return err
	}
	col, err := VectorColumn(len(values))
	if err != nil {
		return err
	}
	sql := fmt.Sprintf("UPDATE t_chunk SET %s = ?, updated_at = now() WHERE id = ?", col)
	res := conn(ctx, r.db).Exec(sql, pgvector.NewVector(values), id)
	if res.Error != nil {
		return fmt.Errorf("回填分块向量失败: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrChunkNotFound
	}
	return nil
}

// SearchByVector 在知识库内对 q_N_vec 做近邻检索；列不存在则空结果。
func (r *ChunkRepository) SearchByVector(ctx context.Context, datasetID int64, values []float32, topK int, minScore float64) ([]model.RetrievalHit, error) {
	col, err := VectorColumn(len(values))
	if err != nil {
		return nil, err
	}
	exists, err := r.vectorColumnExists(ctx, col)
	if err != nil {
		return nil, err
	}
	if !exists {
		return []model.RetrievalHit{}, nil
	}
	var hits []model.RetrievalHit
	vec := pgvector.NewVector(values)
	q := fmt.Sprintf(`
		SELECT c.id AS chunk_id, c.document_id, d.name AS document_name, c.content, c.image_key,
		       1 - (c.%s <=> ?) AS score
		FROM t_chunk c
		JOIN t_document d ON d.id = c.document_id
		WHERE c.dataset_id = ? AND `+retrievalReadySQL+` AND c.%s IS NOT NULL
		ORDER BY c.%s <=> ?
		LIMIT ?`, col, col, col)
	err = conn(ctx, r.db).Raw(q, vec, datasetID, vec, topK).Scan(&hits).Error
	if err != nil {
		return nil, fmt.Errorf("向量检索失败: %w", err)
	}
	if minScore > 0 {
		filtered := hits[:0]
		for _, hit := range hits {
			if hit.Score >= minScore {
				filtered = append(filtered, hit)
			}
		}
		hits = filtered
	}
	return orEmpty(hits), nil
}
