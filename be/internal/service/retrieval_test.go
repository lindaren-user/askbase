package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"askbase/be/internal/model"
)

type retrievalDatasetStore struct {
	dataset model.Dataset
	gets    int
}

func (s *retrievalDatasetStore) Create(context.Context, model.Dataset) (model.Dataset, error) {
	return model.Dataset{}, nil
}

func (s *retrievalDatasetStore) GetByUser(context.Context, int64, int64) (model.Dataset, error) {
	s.gets++
	return s.dataset, nil
}

func (s *retrievalDatasetStore) ListByUser(context.Context, int64) ([]model.Dataset, error) {
	return nil, nil
}

func (s *retrievalDatasetStore) Update(context.Context, int64, int64, *string, *string, *string, *int64, *string) error {
	return nil
}

func (s *retrievalDatasetStore) DeleteByUser(context.Context, int64, int64) error {
	return nil
}

type fakeTextEmbedder struct {
	texts []string
}

func (e *fakeTextEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.texts = append([]string(nil), texts...)
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = []float32{float32(i + 1)}
	}
	return vectors, nil
}

type fakeRetrievalStore struct {
	vectorHits map[int][]model.RetrievalHit
	textHits   map[string][]model.RetrievalHit
	textErrors map[string]error
	minScores  []float64
}

type blockingRetrievalStore struct {
	started chan string
	release chan struct{}
}

func (s *blockingRetrievalStore) SearchByVector(context.Context, int64, []float32, int, float64) ([]model.RetrievalHit, error) {
	s.started <- "vector"
	<-s.release
	return []model.RetrievalHit{{ChunkID: 1}}, nil
}

func (s *blockingRetrievalStore) SearchByFullText(context.Context, int64, string, int) ([]model.RetrievalHit, error) {
	s.started <- "full_text"
	<-s.release
	return []model.RetrievalHit{{ChunkID: 2}}, nil
}

func (s *fakeRetrievalStore) SearchByVector(_ context.Context, _ int64, values []float32, _ int, minScore float64) ([]model.RetrievalHit, error) {
	s.minScores = append(s.minScores, minScore)
	return s.vectorHits[int(values[0])], nil
}

func (s *fakeRetrievalStore) SearchByFullText(_ context.Context, _ int64, query string, _ int) ([]model.RetrievalHit, error) {
	if err := s.textErrors[query]; err != nil {
		return nil, err
	}
	return s.textHits[query], nil
}

func TestRetrievalServiceSearchQueries(t *testing.T) {
	datasets := &retrievalDatasetStore{dataset: model.Dataset{ID: 7}}
	embedder := &fakeTextEmbedder{}
	chunks := &fakeRetrievalStore{
		vectorHits: map[int][]model.RetrievalHit{
			1: {{ChunkID: 1, Score: 0.8}, {ChunkID: 2, Score: 0.7}},
			2: {{ChunkID: 3, Score: 0.9}, {ChunkID: 4, Score: 0.6}},
		},
		textHits: map[string][]model.RetrievalHit{
			"原问题": {{ChunkID: 2, Score: 0.5}, {ChunkID: 3, Score: 0.4}},
			"子问题": {{ChunkID: 3, Score: 0.7}, {ChunkID: 1, Score: 0.3}},
		},
		textErrors: map[string]error{},
	}
	service := &RetrievalService{
		datasets: datasets,
		chunks:   chunks,
		topK:     3,
		minScore: 0.25,
		loadEmbedder: func(context.Context, model.Dataset) (textEmbedder, error) {
			return embedder, nil
		},
	}

	hits, err := service.SearchQueries(
		context.Background(),
		1,
		7,
		[]string{" 原问题 ", "子问题", "原问题"},
		0,
		0,
	)
	if err != nil {
		t.Fatalf("SearchQueries() error = %v", err)
	}
	if datasets.gets != 1 {
		t.Fatalf("dataset gets = %d, want 1", datasets.gets)
	}
	if !reflect.DeepEqual(embedder.texts, []string{"原问题", "子问题"}) {
		t.Fatalf("embedded texts = %#v", embedder.texts)
	}
	if len(hits) != 3 {
		t.Fatalf("len(hits) = %d, want 3", len(hits))
	}
	if hits[0].ChunkID != 3 {
		t.Fatalf("first chunk = %d, want cross-query winner 3", hits[0].ChunkID)
	}
	seen := map[int64]bool{}
	for _, hit := range hits {
		if seen[hit.ChunkID] {
			t.Fatalf("duplicate chunk %d", hit.ChunkID)
		}
		seen[hit.ChunkID] = true
	}
	if !reflect.DeepEqual(chunks.minScores, []float64{0.25, 0.25}) {
		t.Fatalf("min scores = %#v", chunks.minScores)
	}
}

func TestRetrievalServiceFullTextFailureFallsBackPerQuery(t *testing.T) {
	datasets := &retrievalDatasetStore{dataset: model.Dataset{ID: 7}}
	embedder := &fakeTextEmbedder{}
	chunks := &fakeRetrievalStore{
		vectorHits: map[int][]model.RetrievalHit{
			1: {{ChunkID: 1}},
			2: {{ChunkID: 2}},
		},
		textHits: map[string][]model.RetrievalHit{
			"正常": {{ChunkID: 3}},
		},
		textErrors: map[string]error{"降级": errors.New("fts unavailable")},
	}
	service := &RetrievalService{
		datasets: datasets,
		chunks:   chunks,
		topK:     10,
		loadEmbedder: func(context.Context, model.Dataset) (textEmbedder, error) {
			return embedder, nil
		},
	}

	hits, err := service.SearchQueries(context.Background(), 1, 7, []string{"正常", "降级"}, 0, 0)
	if err != nil {
		t.Fatalf("SearchQueries() error = %v", err)
	}
	if len(hits) != 3 {
		t.Fatalf("len(hits) = %d, want 3", len(hits))
	}
}

func TestRetrievalServiceRunsRetrievalRoutesConcurrently(t *testing.T) {
	chunks := &blockingRetrievalStore{
		started: make(chan string, 2),
		release: make(chan struct{}),
	}
	service := &RetrievalService{
		datasets: &retrievalDatasetStore{dataset: model.Dataset{ID: 7}},
		chunks:   chunks,
		topK:     10,
		loadEmbedder: func(context.Context, model.Dataset) (textEmbedder, error) {
			return &fakeTextEmbedder{}, nil
		},
	}

	done := make(chan error, 1)
	go func() {
		_, err := service.SearchQueries(context.Background(), 1, 7, []string{"并行检索"}, 0, 0)
		done <- err
	}()

	for route := 0; route < 2; route++ {
		select {
		case <-chunks.started:
		case <-time.After(time.Second):
			close(chunks.release)
			t.Fatal("向量检索与全文检索未并行启动")
		}
	}
	close(chunks.release)

	if err := <-done; err != nil {
		t.Fatalf("SearchQueries() error = %v", err)
	}
}

func TestRRFMergeDeduplicatesAndLimits(t *testing.T) {
	hits := rrfMerge(
		2,
		[]model.RetrievalHit{{ChunkID: 1}, {ChunkID: 2}},
		[]model.RetrievalHit{{ChunkID: 2}, {ChunkID: 3}},
	)
	if len(hits) != 2 {
		t.Fatalf("len(hits) = %d, want 2", len(hits))
	}
	if hits[0].ChunkID != 2 {
		t.Fatalf("first chunk = %d, want 2", hits[0].ChunkID)
	}
}
