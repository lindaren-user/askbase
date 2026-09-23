package evaluation

import (
	"math"
	"sort"

	"askbase/be/internal/model"
	"askbase/be/internal/service"
)

var queryTypes = []service.QueryType{
	service.QueryTypeNone,
	service.QueryTypeDirect,
	service.QueryTypeContextualRewrite,
	service.QueryTypeDecomposition,
}

// RouteMetrics 汇总路由分类质量与高风险 none 误判。
type RouteMetrics struct {
	Evaluated      int                                             `json:"evaluated"`
	Correct        int                                             `json:"correct"`
	Accuracy       float64                                         `json:"accuracy"`
	MacroF1        float64                                         `json:"macroF1"`
	UnsafeNoneRate float64                                         `json:"unsafeNoneRate"`
	Confusion      map[service.QueryType]map[service.QueryType]int `json:"confusion"`
}

// RetrievalMetrics 汇总有证据用例和无答案用例的检索指标。
type RetrievalMetrics struct {
	EvaluatedEvidence int     `json:"evaluatedEvidence"`
	HitAt1            float64 `json:"hitAt1"`
	HitAt3            float64 `json:"hitAt3"`
	HitAt5            float64 `json:"hitAt5"`
	RecallAt5         float64 `json:"recallAt5"`
	RecallAt10        float64 `json:"recallAt10"`
	MRRAt10           float64 `json:"mrrAt10"`
	NDCGAt10          float64 `json:"ndcgAt10"`
	NegativeEvaluated int     `json:"negativeEvaluated"`
	NegativeNoHitRate float64 `json:"negativeNoHitRate"`
}

// DurationStats 给出毫秒耗时的平均值、中位数和 P95。
type DurationStats struct {
	AverageMS float64 `json:"averageMs"`
	P50MS     float64 `json:"p50Ms"`
	P95MS     float64 `json:"p95Ms"`
}

// PerformanceMetrics 汇总路由、检索与单题总耗时。
type PerformanceMetrics struct {
	Route     DurationStats `json:"route"`
	Retrieval DurationStats `json:"retrieval"`
	Total     DurationStats `json:"total"`
}

// GroupSummary 是按预期路由类型拆分的结果。
type GroupSummary struct {
	Cases     int              `json:"cases"`
	Attempts  int              `json:"attempts"`
	Errors    int              `json:"errors"`
	Route     RouteMetrics     `json:"route"`
	Retrieval RetrievalMetrics `json:"retrieval"`
}

// StabilityMetrics 衡量同一用例多次路由是否得到相同类型。
type StabilityMetrics struct {
	EvaluatedCases int     `json:"evaluatedCases"`
	StableCases    int     `json:"stableCases"`
	Rate           float64 `json:"rate"`
}

// Summary 是报告的总体指标。
type Summary struct {
	Cases       int                                `json:"cases"`
	Attempts    int                                `json:"attempts"`
	Errors      int                                `json:"errors"`
	Route       RouteMetrics                       `json:"route"`
	Retrieval   RetrievalMetrics                   `json:"retrieval"`
	Performance PerformanceMetrics                 `json:"performance"`
	Stability   StabilityMetrics                   `json:"stability"`
	ByRoute     map[service.QueryType]GroupSummary `json:"byRoute"`
}

func evaluateEvidence(result *CaseResult, item Case, hits []model.RetrievalHit) {
	if len(item.RelevantEvidence) == 0 {
		return
	}
	matched := make([]bool, len(item.RelevantEvidence))
	firstRelevantRank := 0
	dcg := 0.0
	for rank, hit := range hits {
		newEvidence := false
		for i, evidence := range item.RelevantEvidence {
			if matched[i] || !evidenceMatchesHit(evidence, hit) {
				continue
			}
			matched[i] = true
			newEvidence = true
			result.EvidenceMatches = append(result.EvidenceMatches, EvidenceMatch{
				EvidenceIndex: i,
				Rank:          rank + 1,
				ChunkID:       hit.ChunkID,
			})
		}
		if newEvidence {
			if firstRelevantRank == 0 {
				firstRelevantRank = rank + 1
			}
			if rank < 10 {
				dcg += 1 / math.Log2(float64(rank)+2)
			}
		}
	}

	result.MissingEvidence = result.MissingEvidence[:0]
	matchedAt5 := 0
	matchedAt10 := 0
	for i, ok := range matched {
		if !ok {
			result.MissingEvidence = append(result.MissingEvidence, item.RelevantEvidence[i])
		}
	}
	for _, match := range result.EvidenceMatches {
		if match.Rank <= 5 {
			matchedAt5++
		}
		if match.Rank <= 10 {
			matchedAt10++
		}
	}
	if firstRelevantRank > 0 {
		if firstRelevantRank <= 1 {
			result.HitAt1 = 1
		}
		if firstRelevantRank <= 3 {
			result.HitAt3 = 1
		}
		if firstRelevantRank <= 5 {
			result.HitAt5 = 1
		}
		if firstRelevantRank <= 10 {
			result.MRRAt10 = 1 / float64(firstRelevantRank)
		}
	}
	result.RecallAt5 = float64(matchedAt5) / float64(len(item.RelevantEvidence))
	result.RecallAt10 = float64(matchedAt10) / float64(len(item.RelevantEvidence))
	result.NDCGAt10 = ndcgAt10(dcg, len(item.RelevantEvidence))
}

func ndcgAt10(dcg float64, relevant int) float64 {
	limit := min(relevant, 10)
	ideal := 0.0
	for rank := 0; rank < limit; rank++ {
		ideal += 1 / math.Log2(float64(rank)+2)
	}
	if ideal == 0 {
		return 0
	}
	value := dcg / ideal
	return min(value, 1)
}

// Summarize 计算总体、按路由类型和稳定性指标。
func Summarize(cases []Case, results []CaseResult) Summary {
	summary := Summary{
		Cases:    len(cases),
		Attempts: len(results),
		ByRoute:  make(map[service.QueryType]GroupSummary, len(queryTypes)),
	}
	caseByID := make(map[string]Case, len(cases))
	for _, item := range cases {
		caseByID[item.ID] = item
	}
	for _, result := range results {
		if result.ExecutionError != "" {
			summary.Errors++
		}
	}
	summary.Route = summarizeRoute(results)
	summary.Retrieval = summarizeRetrieval(caseByID, results)
	summary.Performance = summarizePerformance(results)
	summary.Stability = summarizeStability(results)
	for _, queryType := range queryTypes {
		groupCases := filterCases(cases, queryType)
		groupResults := filterResults(results, queryType)
		errors := 0
		for _, result := range groupResults {
			if result.ExecutionError != "" {
				errors++
			}
		}
		summary.ByRoute[queryType] = GroupSummary{
			Cases:     len(groupCases),
			Attempts:  len(groupResults),
			Errors:    errors,
			Route:     summarizeRoute(groupResults),
			Retrieval: summarizeRetrieval(caseByID, groupResults),
		}
	}
	return summary
}

func summarizeRoute(results []CaseResult) RouteMetrics {
	metrics := RouteMetrics{Confusion: make(map[service.QueryType]map[service.QueryType]int)}
	expectedPresent := make(map[service.QueryType]bool)
	unsafeDenominator := 0
	unsafeCount := 0
	for _, result := range results {
		if result.RouteError != "" {
			continue
		}
		metrics.Evaluated++
		expectedPresent[result.ExpectedRoute] = true
		if metrics.Confusion[result.ExpectedRoute] == nil {
			metrics.Confusion[result.ExpectedRoute] = make(map[service.QueryType]int)
		}
		metrics.Confusion[result.ExpectedRoute][result.ActualRoute]++
		if result.RouteCorrect {
			metrics.Correct++
		}
		if result.ExpectedRoute != service.QueryTypeNone {
			unsafeDenominator++
			if result.ActualRoute == service.QueryTypeNone {
				unsafeCount++
			}
		}
	}
	metrics.Accuracy = ratio(metrics.Correct, metrics.Evaluated)
	metrics.UnsafeNoneRate = ratio(unsafeCount, unsafeDenominator)
	if len(expectedPresent) > 0 {
		f1Total := 0.0
		for queryType := range expectedPresent {
			tp := metrics.Confusion[queryType][queryType]
			fp := 0
			fn := 0
			for expected, row := range metrics.Confusion {
				if expected != queryType {
					fp += row[queryType]
				}
			}
			for actual, count := range metrics.Confusion[queryType] {
				if actual != queryType {
					fn += count
				}
			}
			f1Total += f1(tp, fp, fn)
		}
		metrics.MacroF1 = f1Total / float64(len(expectedPresent))
	}
	return metrics
}

func summarizeRetrieval(caseByID map[string]Case, results []CaseResult) RetrievalMetrics {
	metrics := RetrievalMetrics{}
	for _, result := range results {
		if result.ExecutionError != "" {
			continue
		}
		item := caseByID[result.CaseID]
		if len(item.RelevantEvidence) > 0 {
			metrics.EvaluatedEvidence++
			metrics.HitAt1 += result.HitAt1
			metrics.HitAt3 += result.HitAt3
			metrics.HitAt5 += result.HitAt5
			metrics.RecallAt5 += result.RecallAt5
			metrics.RecallAt10 += result.RecallAt10
			metrics.MRRAt10 += result.MRRAt10
			metrics.NDCGAt10 += result.NDCGAt10
		}
		if item.ExpectNoEvidence {
			metrics.NegativeEvaluated++
			if result.NegativeNoHit {
				metrics.NegativeNoHitRate++
			}
		}
	}
	count := float64(metrics.EvaluatedEvidence)
	if count > 0 {
		metrics.HitAt1 /= count
		metrics.HitAt3 /= count
		metrics.HitAt5 /= count
		metrics.RecallAt5 /= count
		metrics.RecallAt10 /= count
		metrics.MRRAt10 /= count
		metrics.NDCGAt10 /= count
	}
	if metrics.NegativeEvaluated > 0 {
		metrics.NegativeNoHitRate /= float64(metrics.NegativeEvaluated)
	}
	return metrics
}

func summarizePerformance(results []CaseResult) PerformanceMetrics {
	route := make([]float64, 0, len(results))
	retrieval := make([]float64, 0, len(results))
	total := make([]float64, 0, len(results))
	for _, result := range results {
		route = append(route, result.RouteDurationMS)
		total = append(total, result.TotalDurationMS)
		if result.RetrievalPerformed {
			retrieval = append(retrieval, result.RetrievalDurationMS)
		}
	}
	return PerformanceMetrics{
		Route:     durationStats(route),
		Retrieval: durationStats(retrieval),
		Total:     durationStats(total),
	}
}

func summarizeStability(results []CaseResult) StabilityMetrics {
	byCase := make(map[string][]service.QueryType)
	for _, result := range results {
		if result.RouteError == "" {
			byCase[result.CaseID] = append(byCase[result.CaseID], result.ActualRoute)
		}
	}
	metrics := StabilityMetrics{EvaluatedCases: len(byCase)}
	for _, routes := range byCase {
		stable := true
		for _, route := range routes[1:] {
			if route != routes[0] {
				stable = false
				break
			}
		}
		if stable {
			metrics.StableCases++
		}
	}
	metrics.Rate = ratio(metrics.StableCases, metrics.EvaluatedCases)
	return metrics
}

func durationStats(values []float64) DurationStats {
	if len(values) == 0 {
		return DurationStats{}
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	total := 0.0
	for _, value := range sorted {
		total += value
	}
	return DurationStats{
		AverageMS: total / float64(len(sorted)),
		P50MS:     percentile(sorted, 0.50),
		P95MS:     percentile(sorted, 0.95),
	}
}

func percentile(sorted []float64, percentile float64) float64 {
	index := int(math.Ceil(percentile*float64(len(sorted)))) - 1
	index = max(0, min(index, len(sorted)-1))
	return sorted[index]
}

func f1(tp int, fp int, fn int) float64 {
	denominator := 2*tp + fp + fn
	if denominator == 0 {
		return 0
	}
	return float64(2*tp) / float64(denominator)
}

func ratio(numerator int, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func filterCases(cases []Case, queryType service.QueryType) []Case {
	filtered := make([]Case, 0)
	for _, item := range cases {
		if item.ExpectedRoute == queryType {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func filterResults(results []CaseResult, queryType service.QueryType) []CaseResult {
	filtered := make([]CaseResult, 0)
	for _, result := range results {
		if result.ExpectedRoute == queryType {
			filtered = append(filtered, result)
		}
	}
	return filtered
}
