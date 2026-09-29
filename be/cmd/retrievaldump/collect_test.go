package main

import (
	"context"
	"testing"

	"askbase/be/internal/model"
	"askbase/be/internal/service"
)

type testRetriever struct {
	calls   int
	queries []string
	hits    []model.RetrievalHit
}

// SearchQueries 记录测试查询，并返回预设命中结果。
func (r *testRetriever) SearchQueries(_ context.Context, _, _ int64, queries []string, _ int, _ float64) ([]model.RetrievalHit, error) {
	r.calls++
	r.queries = queries
	return r.hits, nil
}

type testPlanner struct{ plan service.QueryPlan }

// Plan 返回预设规划，用于验证采集器对不同查询类型的处理。
func (p testPlanner) Plan(context.Context, service.QueryRouteInput) (service.QueryPlan, error) {
	return p.plan, nil
}

// TestCollectOneDirectPreservesSourceIDsAndRanking 验证直接检索保留公开 ID 与命中顺序。
func TestCollectOneDirectPreservesSourceIDsAndRanking(t *testing.T) {
	retrieval := &testRetriever{hits: []model.RetrievalHit{
		{ChunkID: 99, DocumentID: 7, Score: 0.8},
		{ChunkID: 88, DocumentID: 8, Score: 0.6},
	}}
	result, err := collectOne(context.Background(), model.Dataset{ID: 2, UserID: 3}, retrieval,
		nil, map[int64]string{7: "source-a", 8: "source-b"},
		queryRow{ID: "q1", Text: "test query"}, "direct", 10, 0.2)
	if err != nil {
		t.Fatal(err)
	}
	if retrieval.calls != 1 || len(retrieval.queries) != 1 || retrieval.queries[0] != "test query" {
		t.Fatalf("unexpected retrieval: %+v", retrieval)
	}
	if len(result.Hits) != 2 || result.Hits[0].DocumentID != "source-a" ||
		result.Hits[0].Rank != 1 || result.Hits[1].DocumentID != "source-b" {
		t.Fatalf("unexpected ranking: %+v", result.Hits)
	}
}

// TestCollectOnePlannedNoneSkipsRetrieval 验证无需检索的规划不会调用检索服务。
func TestCollectOnePlannedNoneSkipsRetrieval(t *testing.T) {
	retrieval := &testRetriever{}
	result, err := collectOne(context.Background(), model.Dataset{ID: 2}, retrieval,
		testPlanner{plan: service.QueryPlan{QueryType: service.QueryTypeNone}}, nil,
		queryRow{ID: "q1", Text: "hello"}, "planned", 10, 0.2)
	if err != nil || retrieval.calls != 0 || len(result.Hits) != 0 {
		t.Fatalf("unexpected result: %+v, calls %d, error %v", result, retrieval.calls, err)
	}
}

// TestCollectOneRejectsUnmappedDocuments 验证基准语料之外的命中会导致采集失败。
func TestCollectOneRejectsUnmappedDocuments(t *testing.T) {
	retrieval := &testRetriever{hits: []model.RetrievalHit{{DocumentID: 7}}}
	_, err := collectOne(context.Background(), model.Dataset{ID: 2}, retrieval, nil,
		map[int64]string{}, queryRow{ID: "q1", Text: "question"}, "direct", 10, 0.2)
	if err == nil {
		t.Fatal("expected unmapped document error")
	}
}
