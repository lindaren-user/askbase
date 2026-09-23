package evaluation

import (
	"math"
	"testing"

	"askbase/be/internal/model"
	"askbase/be/internal/service"
)

func TestEvaluateEvidence(t *testing.T) {
	item := Case{
		RelevantEvidence: []Evidence{
			{DocumentName: "说明.pdf", Contains: []string{"2024年", "上线"}},
			{DocumentName: "部署.pdf", Contains: []string{"生产环境"}},
		},
	}
	hits := []model.RetrievalHit{
		{ChunkID: 1, DocumentName: "其他.pdf", Content: "无关"},
		{ChunkID: 2, DocumentName: "说明.pdf", Content: "项目于2024年正式上线"},
		{ChunkID: 3, DocumentName: "说明.pdf", Content: "2024年上线"},
		{ChunkID: 4, DocumentName: "部署.pdf", Content: "部署在生产环境"},
	}
	result := CaseResult{MissingEvidence: append([]Evidence(nil), item.RelevantEvidence...)}
	evaluateEvidence(&result, item, hits)

	if result.HitAt1 != 0 || result.HitAt3 != 1 || result.HitAt5 != 1 {
		t.Fatalf("unexpected hits: %.0f %.0f %.0f", result.HitAt1, result.HitAt3, result.HitAt5)
	}
	if result.RecallAt5 != 1 || result.RecallAt10 != 1 {
		t.Fatalf("unexpected recall: %f %f", result.RecallAt5, result.RecallAt10)
	}
	if result.MRRAt10 != 0.5 {
		t.Fatalf("MRR@10 = %f, want 0.5", result.MRRAt10)
	}
	if len(result.EvidenceMatches) != 2 || len(result.MissingEvidence) != 0 {
		t.Fatalf("unexpected evidence result: matches=%#v missing=%#v", result.EvidenceMatches, result.MissingEvidence)
	}
	if result.NDCGAt10 <= 0 || result.NDCGAt10 > 1 {
		t.Fatalf("NDCG@10 = %f, want (0, 1]", result.NDCGAt10)
	}
}

func TestSummarizeSeparatesErrorsAndUnsafeNone(t *testing.T) {
	cases := []Case{
		{ID: "none", ExpectedRoute: service.QueryTypeNone},
		{ID: "direct", ExpectedRoute: service.QueryTypeDirect, RelevantEvidence: []Evidence{{DocumentName: "a", Contains: []string{"x"}}}},
		{ID: "rewrite", ExpectedRoute: service.QueryTypeContextualRewrite, RelevantEvidence: []Evidence{{DocumentName: "b", Contains: []string{"y"}}}},
		{ID: "error", ExpectedRoute: service.QueryTypeDecomposition},
	}
	results := []CaseResult{
		{CaseID: "none", ExpectedRoute: service.QueryTypeNone, ActualRoute: service.QueryTypeNone, RouteCorrect: true},
		{CaseID: "direct", ExpectedRoute: service.QueryTypeDirect, ActualRoute: service.QueryTypeNone},
		{CaseID: "rewrite", ExpectedRoute: service.QueryTypeContextualRewrite, ActualRoute: service.QueryTypeContextualRewrite, RouteCorrect: true, HitAt5: 1, RecallAt10: 0.5, MRRAt10: 1},
		{
			CaseID:         "error",
			ExpectedRoute:  service.QueryTypeDecomposition,
			ActualRoute:    service.QueryTypeDirect,
			RouteError:     "provider failed",
			ExecutionError: "provider failed",
		},
	}
	summary := Summarize(cases, results)

	if summary.Errors != 1 || summary.Route.Evaluated != 3 || summary.Route.Correct != 2 {
		t.Fatalf("unexpected summary counts: %#v", summary)
	}
	if math.Abs(summary.Route.Accuracy-2.0/3.0) > 1e-9 {
		t.Fatalf("accuracy = %f", summary.Route.Accuracy)
	}
	if math.Abs(summary.Route.UnsafeNoneRate-0.5) > 1e-9 {
		t.Fatalf("unsafe none rate = %f", summary.Route.UnsafeNoneRate)
	}
	if summary.Retrieval.EvaluatedEvidence != 2 || summary.Retrieval.HitAt5 != 0.5 {
		t.Fatalf("unexpected retrieval summary: %#v", summary.Retrieval)
	}
}

func TestSummarizeKeepsRouteResultWhenOnlyRetrievalFails(t *testing.T) {
	cases := []Case{{ID: "direct", ExpectedRoute: service.QueryTypeDirect}}
	results := []CaseResult{{
		CaseID:         "direct",
		ExpectedRoute:  service.QueryTypeDirect,
		ActualRoute:    service.QueryTypeDirect,
		RouteCorrect:   true,
		RetrievalError: "database unavailable",
		ExecutionError: "database unavailable",
	}}

	summary := Summarize(cases, results)

	if summary.Errors != 1 || summary.Route.Evaluated != 1 || summary.Route.Accuracy != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if summary.Retrieval.EvaluatedEvidence != 0 {
		t.Fatalf("retrieval errors must not enter retrieval metrics: %#v", summary.Retrieval)
	}
}

func TestDurationStats(t *testing.T) {
	stats := durationStats([]float64{50, 10, 30, 20})
	if stats.AverageMS != 27.5 || stats.P50MS != 20 || stats.P95MS != 50 {
		t.Fatalf("durationStats() = %#v", stats)
	}
}
