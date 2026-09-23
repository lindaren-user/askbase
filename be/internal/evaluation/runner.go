package evaluation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"askbase/be/internal/model"
	"askbase/be/internal/service"
)

type queryPlanner interface {
	Plan(ctx context.Context, input service.QueryRouteInput) (service.QueryPlan, error)
}

type retriever interface {
	SearchQueries(ctx context.Context, userID int64, datasetID int64, queries []string, topK int, minScore float64) ([]model.RetrievalHit, error)
}

type datasetResolver interface {
	GetByID(ctx context.Context, id int64) (model.Dataset, error)
}

// Config 控制评测重复次数、检索参数与报告中的安全配置快照。
type Config struct {
	Runs           int            `json:"runs"`
	TopK           int            `json:"topK"`
	MinScore       float64        `json:"minScore"`
	ConfigSnapshot map[string]any `json:"configSnapshot"`
}

// RetrievedChunk 是报告中保留的召回分块信息。
type RetrievedChunk struct {
	Rank         int     `json:"rank"`
	ChunkID      int64   `json:"chunkId,string"`
	DocumentID   int64   `json:"documentId"`
	DocumentName string  `json:"documentName"`
	Content      string  `json:"content"`
	Score        float64 `json:"score"`
}

// EvidenceMatch 记录一条黄金证据首次命中的召回位置。
type EvidenceMatch struct {
	EvidenceIndex int   `json:"evidenceIndex"`
	Rank          int   `json:"rank"`
	ChunkID       int64 `json:"chunkId,string"`
}

// CaseResult 是一条用例在一次运行中的完整结果。
type CaseResult struct {
	Run                 int               `json:"run"`
	CaseID              string            `json:"caseId"`
	ExpectedRoute       service.QueryType `json:"expectedRoute"`
	ExpectNoEvidence    bool              `json:"expectNoEvidence"`
	ActualRoute         service.QueryType `json:"actualRoute"`
	RouteCorrect        bool              `json:"routeCorrect"`
	Fallback            bool              `json:"fallback"`
	Queries             []string          `json:"queries"`
	RetrievalPerformed  bool              `json:"retrievalPerformed"`
	RetrievedChunks     []RetrievedChunk  `json:"retrievedChunks"`
	EvidenceMatches     []EvidenceMatch   `json:"evidenceMatches"`
	MissingEvidence     []Evidence        `json:"missingEvidence"`
	HitAt1              float64           `json:"hitAt1"`
	HitAt3              float64           `json:"hitAt3"`
	HitAt5              float64           `json:"hitAt5"`
	RecallAt5           float64           `json:"recallAt5"`
	RecallAt10          float64           `json:"recallAt10"`
	MRRAt10             float64           `json:"mrrAt10"`
	NDCGAt10            float64           `json:"ndcgAt10"`
	NegativeNoHit       bool              `json:"negativeNoHit"`
	RouteDurationMS     float64           `json:"routeDurationMs"`
	RetrievalDurationMS float64           `json:"retrievalDurationMs"`
	TotalDurationMS     float64           `json:"totalDurationMs"`
	RouteError          string            `json:"routeError,omitempty"`
	RetrievalError      string            `json:"retrievalError,omitempty"`
	ExecutionError      string            `json:"executionError,omitempty"`
}

// Report 汇总一次完整评测的配置、指标与逐条结果。
type Report struct {
	GeneratedAt time.Time    `json:"generatedAt"`
	Config      Config       `json:"config"`
	Summary     Summary      `json:"summary"`
	Results     []CaseResult `json:"results"`
}

// Runner 使用线上同款查询计划与检索能力执行离线黄金集。
type Runner struct {
	planner   queryPlanner
	retrieval retriever
	datasets  datasetResolver
	config    Config
}

// NewRunner 创建评测运行器。
func NewRunner(
	planner queryPlanner,
	retrieval retriever,
	datasets datasetResolver,
	config Config,
) *Runner {
	if config.Runs <= 0 {
		config.Runs = 1
	}
	return &Runner{
		planner:   planner,
		retrieval: retrieval,
		datasets:  datasets,
		config:    config,
	}
}

// Run 顺序执行全部用例；单条错误会写入结果但不会中断整批评测。
func (r *Runner) Run(ctx context.Context, cases []Case) Report {
	results := make([]CaseResult, 0, len(cases)*r.config.Runs)
	for run := 1; run <= r.config.Runs; run++ {
		for _, item := range cases {
			results = append(results, r.runCase(ctx, run, item))
		}
	}
	return Report{
		GeneratedAt: time.Now(),
		Config:      r.config,
		Summary:     Summarize(cases, results),
		Results:     results,
	}
}

func (r *Runner) runCase(ctx context.Context, run int, item Case) CaseResult {
	totalStarted := time.Now()
	result := CaseResult{
		Run:              run,
		CaseID:           item.ID,
		ExpectedRoute:    item.ExpectedRoute,
		ExpectNoEvidence: item.ExpectNoEvidence,
		Queries:          []string{},
		RetrievedChunks:  []RetrievedChunk{},
		EvidenceMatches:  []EvidenceMatch{},
		MissingEvidence:  append([]Evidence(nil), item.RelevantEvidence...),
	}

	routeStarted := time.Now()
	plan, planErr := r.planner.Plan(ctx, service.QueryRouteInput{
		Question: item.Question,
		History:  item.History,
	})
	result.RouteDurationMS = durationMS(time.Since(routeStarted))
	result.ActualRoute = plan.QueryType
	result.RouteCorrect = plan.QueryType == item.ExpectedRoute
	result.Fallback = plan.Fallback
	result.Queries = append([]string(nil), plan.Queries...)
	if planErr != nil {
		result.RouteError = fmt.Sprintf("查询计划失败: %v", planErr)
		result.ExecutionError = result.RouteError
	}

	if plan.QueryType != service.QueryTypeNone && len(plan.Queries) > 0 {
		result.RetrievalPerformed = true
		dataset, err := r.datasets.GetByID(ctx, item.DatasetID)
		if err != nil {
			result.RetrievalError = fmt.Sprintf("加载知识库失败: %v", err)
			result.ExecutionError = joinErrors(result.ExecutionError, result.RetrievalError)
			result.TotalDurationMS = durationMS(time.Since(totalStarted))
			return result
		}
		retrievalStarted := time.Now()
		hits, err := r.retrieval.SearchQueries(
			ctx,
			dataset.UserID,
			item.DatasetID,
			plan.Queries,
			r.config.TopK,
			r.config.MinScore,
		)
		result.RetrievalDurationMS = durationMS(time.Since(retrievalStarted))
		if err != nil {
			result.RetrievalError = fmt.Sprintf("检索失败: %v", err)
			result.ExecutionError = joinErrors(result.ExecutionError, result.RetrievalError)
			result.TotalDurationMS = durationMS(time.Since(totalStarted))
			return result
		}
		result.RetrievedChunks = reportChunks(hits)
		evaluateEvidence(&result, item, hits)
	}
	if item.ExpectNoEvidence {
		result.NegativeNoHit = len(result.RetrievedChunks) == 0
	}
	result.TotalDurationMS = durationMS(time.Since(totalStarted))
	return result
}

func reportChunks(hits []model.RetrievalHit) []RetrievedChunk {
	chunks := make([]RetrievedChunk, 0, len(hits))
	for i, hit := range hits {
		chunks = append(chunks, RetrievedChunk{
			Rank:         i + 1,
			ChunkID:      hit.ChunkID,
			DocumentID:   hit.DocumentID,
			DocumentName: hit.DocumentName,
			Content:      hit.Content,
			Score:        hit.Score,
		})
	}
	return chunks
}

func evidenceMatchesHit(evidence Evidence, hit model.RetrievalHit) bool {
	if !strings.EqualFold(strings.TrimSpace(evidence.DocumentName), strings.TrimSpace(hit.DocumentName)) {
		return false
	}
	content := strings.ToLower(hit.Content)
	for _, marker := range evidence.Contains {
		if !strings.Contains(content, strings.ToLower(strings.TrimSpace(marker))) {
			return false
		}
	}
	return true
}

func joinErrors(current string, next string) string {
	if current == "" {
		return next
	}
	return current + "; " + next
}

func durationMS(duration time.Duration) float64 {
	return float64(duration.Microseconds()) / 1000
}
