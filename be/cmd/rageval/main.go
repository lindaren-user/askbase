package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"askbase/be/internal/config"
	"askbase/be/internal/evaluation"
	"askbase/be/internal/infra"
	"askbase/be/internal/llm"
	"askbase/be/internal/logging"
	"askbase/be/internal/repo"
	"askbase/be/internal/service"

	"go.uber.org/zap"
)

type options struct {
	casesPath string
	outputDir string
	runs      int
	topK      int
	minScore  float64
}

// main 加载真实服务依赖并执行本地路由与检索评测，不启动 HTTP 服务。
func main() {
	if err := run(); err != nil {
		log.Printf("rag evaluation failed: %v", err)
		os.Exit(1)
	}
}

func run() error {
	opts := parseOptions()
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	if err := logging.Init(cfg.Log); err != nil {
		return fmt.Errorf("初始化日志失败: %w", err)
	}
	defer func() { _ = zap.L().Sync() }()

	cases, err := evaluation.LoadCases(opts.casesPath)
	if err != nil {
		return err
	}
	if opts.topK <= 0 {
		opts.topK = cfg.Chat.Retrieval.TopK
	}
	if opts.minScore <= 0 {
		opts.minScore = cfg.Chat.Retrieval.MinScore
	}

	connectCtx, connectCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer connectCancel()
	db, err := infra.NewPostgres(connectCtx, cfg.Postgres)
	if err != nil {
		return fmt.Errorf("连接数据库失败: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("获取数据库连接池失败: %w", err)
	}
	defer sqlDB.Close()

	llmClient := llm.New(
		cfg.LLM.APIURL,
		cfg.LLM.APIKey,
		cfg.LLM.Model,
		cfg.Embedding.APIURL,
		cfg.Embedding.APIKey,
		cfg.Embedding.Model,
	)
	repositories := repo.NewRepositories(db)
	router := service.NewLLMQueryRouter(llmClient, cfg.Chat.Router)
	planner := service.NewQueryPlanner(router, cfg.Chat.Router.MaxSubqueries)
	retrieval := service.NewRetrievalService(
		repositories.Datasets,
		repositories.EmbedModels,
		repositories.Chunks,
		nil,
		cfg.Chat.Retrieval,
		cfg.Embedding,
	)
	runner := evaluation.NewRunner(
		planner,
		retrieval,
		repositories.Datasets,
		evaluation.Config{
			Runs:           opts.runs,
			TopK:           opts.topK,
			MinScore:       opts.minScore,
			ConfigSnapshot: configSnapshot(cfg, opts),
		},
	)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	report := runner.Run(ctx, cases)
	if err := evaluation.WriteReports(opts.outputDir, report); err != nil {
		return err
	}
	fmt.Printf("评测完成：%d 条用例，%d 次执行，%d 个执行错误\n", report.Summary.Cases, report.Summary.Attempts, report.Summary.Errors)
	fmt.Printf("路由 Accuracy=%.4f Macro-F1=%.4f UnsafeNone=%.4f\n", report.Summary.Route.Accuracy, report.Summary.Route.MacroF1, report.Summary.Route.UnsafeNoneRate)
	fmt.Printf("检索 Hit@5=%.4f Recall@10=%.4f MRR@10=%.4f NDCG@10=%.4f\n", report.Summary.Retrieval.HitAt5, report.Summary.Retrieval.RecallAt10, report.Summary.Retrieval.MRRAt10, report.Summary.Retrieval.NDCGAt10)
	fmt.Printf("报告目录：%s\n", opts.outputDir)
	return nil
}

func parseOptions() options {
	var opts options
	flag.StringVar(&opts.casesPath, "cases", "./eval/cases.jsonl", "JSONL 黄金测试集路径")
	flag.StringVar(&opts.outputDir, "output", "./eval/results", "JSON 和 Markdown 报告输出目录")
	flag.IntVar(&opts.runs, "runs", 1, "每条用例重复运行次数")
	flag.IntVar(&opts.topK, "top-k", 0, "检索返回数量，0 表示使用配置值")
	flag.Float64Var(&opts.minScore, "min-score", 0, "向量相似度阈值，0 表示使用配置值")
	flag.Parse()
	if opts.runs < 1 {
		log.Fatal("--runs 必须大于或等于 1")
	}
	return opts
}

func configSnapshot(cfg config.Config, opts options) map[string]any {
	return map[string]any{
		"llm": map[string]any{
			"apiUrl": cfg.LLM.APIURL,
			"model":  cfg.LLM.Model,
		},
		"embedding": map[string]any{
			"apiUrl":    cfg.Embedding.APIURL,
			"model":     cfg.Embedding.Model,
			"dimension": cfg.Embedding.Dimension,
		},
		"router": map[string]any{
			"maxTokens":     cfg.Chat.Router.MaxTokens,
			"maxSubqueries": cfg.Chat.Router.MaxSubqueries,
		},
		"retrieval": map[string]any{
			"topK":     opts.topK,
			"minScore": opts.minScore,
		},
	}
}
