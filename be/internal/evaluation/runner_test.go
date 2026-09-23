package evaluation

import (
	"context"
	"errors"
	"testing"

	"askbase/be/internal/model"
	"askbase/be/internal/service"
)

type fakePlanner struct {
	plan service.QueryPlan
	err  error
}

func (p fakePlanner) Plan(context.Context, service.QueryRouteInput) (service.QueryPlan, error) {
	return p.plan, p.err
}

type fakeRetriever struct {
	calls int
	hits  []model.RetrievalHit
	err   error
}

func (r *fakeRetriever) SearchQueries(context.Context, int64, int64, []string, int, float64) ([]model.RetrievalHit, error) {
	r.calls++
	return r.hits, r.err
}

type fakeDatasetResolver struct {
	dataset model.Dataset
	err     error
}

func (r fakeDatasetResolver) GetByID(context.Context, int64) (model.Dataset, error) {
	return r.dataset, r.err
}

func TestRunnerSkipsRetrievalForNone(t *testing.T) {
	retrieval := &fakeRetriever{}
	runner := NewRunner(
		fakePlanner{plan: service.QueryPlan{QueryType: service.QueryTypeNone, Queries: []string{}}},
		retrieval,
		fakeDatasetResolver{},
		Config{Runs: 2},
	)
	report := runner.Run(context.Background(), []Case{{
		ID:            "hello",
		Question:      "你好",
		ExpectedRoute: service.QueryTypeNone,
	}})
	if retrieval.calls != 0 {
		t.Fatalf("retrieval calls = %d, want 0", retrieval.calls)
	}
	if len(report.Results) != 2 || report.Summary.Stability.Rate != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestRunnerContinuesAfterExecutionError(t *testing.T) {
	runner := NewRunner(
		fakePlanner{
			plan: service.QueryPlan{QueryType: service.QueryTypeDirect, Queries: []string{"问题"}, Fallback: true},
			err:  errors.New("router unavailable"),
		},
		&fakeRetriever{hits: []model.RetrievalHit{{ChunkID: 1, DocumentName: "说明.pdf", Content: "目标证据"}}},
		fakeDatasetResolver{dataset: model.Dataset{ID: 7, UserID: 9}},
		Config{Runs: 1, TopK: 10},
	)
	report := runner.Run(context.Background(), []Case{{
		ID:               "fact",
		DatasetID:        7,
		Question:         "问题",
		ExpectedRoute:    service.QueryTypeDirect,
		RelevantEvidence: []Evidence{{DocumentName: "说明.pdf", Contains: []string{"目标证据"}}},
	}})
	if len(report.Results) != 1 || report.Results[0].ExecutionError == "" {
		t.Fatalf("expected recorded execution error: %#v", report.Results)
	}
	if len(report.Results[0].RetrievedChunks) != 1 {
		t.Fatal("fallback plan should still execute retrieval")
	}
	if report.Summary.Errors != 1 || report.Summary.Route.Evaluated != 0 {
		t.Fatalf("unexpected error aggregation: %#v", report.Summary)
	}
}
