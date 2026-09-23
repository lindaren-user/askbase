package evaluation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// WriteReports 将完整 JSON 和便于阅读的 Markdown 报告写入目标目录。
func WriteReports(outputDir string, report Report) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("创建报告目录失败: %w", err)
	}
	jsonData, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 JSON 报告失败: %w", err)
	}
	jsonData = append(jsonData, '\n')
	if err := os.WriteFile(filepath.Join(outputDir, "report.json"), jsonData, 0o600); err != nil {
		return fmt.Errorf("写入 JSON 报告失败: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "report.md"), []byte(renderMarkdown(report)), 0o600); err != nil {
		return fmt.Errorf("写入 Markdown 报告失败: %w", err)
	}
	return nil
}

func renderMarkdown(report Report) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# AskBase 路由与检索评测报告\n\n")
	fmt.Fprintf(&builder, "- 生成时间：%s\n", report.GeneratedAt.Format("2006-01-02 15:04:05 -07:00"))
	fmt.Fprintf(&builder, "- 用例数：%d\n", report.Summary.Cases)
	fmt.Fprintf(&builder, "- 执行次数：%d\n", report.Summary.Attempts)
	fmt.Fprintf(&builder, "- 执行错误：%d\n\n", report.Summary.Errors)

	builder.WriteString("## 总体指标\n\n")
	fmt.Fprintf(&builder, "- 路由准确率：%.4f\n", report.Summary.Route.Accuracy)
	fmt.Fprintf(&builder, "- 路由 Macro-F1：%.4f\n", report.Summary.Route.MacroF1)
	fmt.Fprintf(&builder, "- 危险 none 误判率：%.4f\n", report.Summary.Route.UnsafeNoneRate)
	fmt.Fprintf(&builder, "- Hit@1 / Hit@3 / Hit@5：%.4f / %.4f / %.4f\n", report.Summary.Retrieval.HitAt1, report.Summary.Retrieval.HitAt3, report.Summary.Retrieval.HitAt5)
	fmt.Fprintf(&builder, "- Recall@5 / Recall@10：%.4f / %.4f\n", report.Summary.Retrieval.RecallAt5, report.Summary.Retrieval.RecallAt10)
	fmt.Fprintf(&builder, "- MRR@10 / NDCG@10：%.4f / %.4f\n", report.Summary.Retrieval.MRRAt10, report.Summary.Retrieval.NDCGAt10)
	fmt.Fprintf(&builder, "- 无答案用例零召回率：%.4f\n", report.Summary.Retrieval.NegativeNoHitRate)
	fmt.Fprintf(&builder, "- 路由稳定率：%.4f\n\n", report.Summary.Stability.Rate)

	builder.WriteString("## 性能\n\n")
	builder.WriteString("| 阶段 | 平均 | P50 | P95 |\n| --- | ---: | ---: | ---: |\n")
	writeDurationRow(&builder, "路由", report.Summary.Performance.Route)
	writeDurationRow(&builder, "检索", report.Summary.Performance.Retrieval)
	writeDurationRow(&builder, "总计", report.Summary.Performance.Total)

	builder.WriteString("\n## 路由混淆矩阵\n\n")
	builder.WriteString("| 预期 \\ 实际 | none | direct | contextual_rewrite | decomposition |\n")
	builder.WriteString("| --- | ---: | ---: | ---: | ---: |\n")
	for _, expected := range queryTypes {
		fmt.Fprintf(&builder, "| %s", expected)
		for _, actual := range queryTypes {
			fmt.Fprintf(&builder, " | %d", report.Summary.Route.Confusion[expected][actual])
		}
		builder.WriteString(" |\n")
	}

	builder.WriteString("\n## 分类结果\n\n")
	builder.WriteString("| 类型 | 用例 | 执行错误 | 路由准确率 | Hit@5 | Recall@10 | MRR@10 |\n")
	builder.WriteString("| --- | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, queryType := range queryTypes {
		group := report.Summary.ByRoute[queryType]
		fmt.Fprintf(
			&builder,
			"| %s | %d | %d | %.4f | %.4f | %.4f | %.4f |\n",
			queryType,
			group.Cases,
			group.Errors,
			group.Route.Accuracy,
			group.Retrieval.HitAt5,
			group.Retrieval.RecallAt10,
			group.Retrieval.MRRAt10,
		)
	}

	builder.WriteString("\n## 配置快照\n\n")
	configData, err := json.MarshalIndent(report.Config.ConfigSnapshot, "", "  ")
	if err == nil {
		builder.WriteString("```json\n")
		builder.Write(configData)
		builder.WriteString("\n```\n")
	}

	builder.WriteString("\n## 失败明细\n\n")
	failures := failureResults(report.Results)
	if len(failures) == 0 {
		builder.WriteString("没有失败用例。\n")
		return builder.String()
	}
	for _, result := range failures {
		fmt.Fprintf(&builder, "### %s（第 %d 次）\n\n", result.CaseID, result.Run)
		fmt.Fprintf(&builder, "- 预期路由：`%s`\n", result.ExpectedRoute)
		fmt.Fprintf(&builder, "- 实际路由：`%s`\n", result.ActualRoute)
		fmt.Fprintf(&builder, "- 查询计划：`%s`\n", strings.Join(result.Queries, "`、`"))
		if result.ExecutionError != "" {
			fmt.Fprintf(&builder, "- 执行错误：%s\n", result.ExecutionError)
		}
		if len(result.MissingEvidence) > 0 {
			builder.WriteString("- 未命中证据：\n")
			for _, evidence := range result.MissingEvidence {
				fmt.Fprintf(&builder, "  - `%s`：%s\n", evidence.DocumentName, strings.Join(evidence.Contains, " / "))
			}
		}
		if len(result.RetrievedChunks) > 0 {
			builder.WriteString("- 召回结果：\n")
			for _, chunk := range result.RetrievedChunks {
				fmt.Fprintf(&builder, "  - #%d `%s` chunk=%d score=%.4f：%s\n", chunk.Rank, chunk.DocumentName, chunk.ChunkID, chunk.Score, compactText(chunk.Content, 160))
			}
		}
		builder.WriteString("\n")
	}
	return builder.String()
}

func failureResults(results []CaseResult) []CaseResult {
	failures := make([]CaseResult, 0)
	for _, result := range results {
		failedEvidence := len(result.MissingEvidence) > 0
		failedNegative := result.ExpectNoEvidence && !result.NegativeNoHit
		if result.ExecutionError != "" || !result.RouteCorrect || failedEvidence || failedNegative {
			failures = append(failures, result)
		}
	}
	sort.SliceStable(failures, func(i, j int) bool {
		if failures[i].CaseID == failures[j].CaseID {
			return failures[i].Run < failures[j].Run
		}
		return failures[i].CaseID < failures[j].CaseID
	})
	return failures
}

func writeDurationRow(builder *strings.Builder, name string, stats DurationStats) {
	fmt.Fprintf(builder, "| %s | %.2fms | %.2fms | %.2fms |\n", name, stats.AverageMS, stats.P50MS, stats.P95MS)
}

func compactText(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
