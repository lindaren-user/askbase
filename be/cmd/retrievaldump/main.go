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
	"askbase/be/internal/infra"
	"askbase/be/internal/llm"
	"askbase/be/internal/model"
	"askbase/be/internal/repo"
	"askbase/be/internal/service"
)

// main 执行检索基准命令，并将启动或运行错误写入日志。
func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

// run 解析 index 或 collect 参数，加载知识库配置并调用对应的评测流程。
func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: retrievaldump index|collect [flags]")
	}
	command := os.Args[1]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	input := flags.String("input", "", "corpus or queries JSONL")
	output := flags.String("output", "", "collection JSON output (collect only)")
	datasetID := flags.Int64("dataset-id", 0, "dedicated benchmark dataset ID")
	mode := flags.String("mode", "direct", "direct or planned (collect only)")
	topK := flags.Int("top-k", 10, "number of retrieval hits, maximum 50")
	minScore := flags.Float64("min-score", -1, "minimum vector score; -1 uses application config")
	rebuild := flags.Bool("rebuild", false, "reindex existing benchmark passages (index only)")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	if *input == "" || *datasetID <= 0 {
		return fmt.Errorf("--input and --dataset-id are required")
	}
	if command != "index" && command != "collect" {
		return fmt.Errorf("unknown command %q", command)
	}
	if command == "collect" && (*output == "" || (*mode != "direct" && *mode != "planned") || *topK < 1 || *topK > 50) {
		return fmt.Errorf("collect requires --output, --mode direct|planned and --top-k in 1..50")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	db, err := infra.NewPostgres(connectCtx, cfg.Postgres)
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	repositories := repo.NewRepositories(db)
	dataset, err := repositories.Datasets.GetByID(ctx, *datasetID)
	if err != nil {
		return err
	}
	if command == "index" {
		embedder, err := benchmarkEmbedder(ctx, cfg, dataset, repositories)
		if err != nil {
			return err
		}
		return indexCorpus(ctx, db, repositories.Chunks, dataset, embedder, *input, *rebuild)
	}
	if *minScore < 0 {
		*minScore = cfg.Chat.Retrieval.MinScore
	}
	if *minScore <= 0 {
		return fmt.Errorf("--min-score must be positive for the current retrieval service")
	}
	llmClient := llm.New(cfg.LLM.APIURL, cfg.LLM.APIKey, cfg.LLM.Model,
		cfg.Embedding.APIURL, cfg.Embedding.APIKey, cfg.Embedding.Model)
	retriever := service.NewRetrievalService(repositories.Datasets, repositories.EmbedModels,
		repositories.Chunks, nil, cfg.Chat.Retrieval, cfg.Embedding)
	var planner service.QueryPlanner
	if *mode == "planned" {
		planner = service.NewQueryPlanner(service.NewLLMQueryRouter(llmClient, cfg.Chat.Router), cfg.Chat.Router.MaxSubqueries)
	}
	return collect(ctx, db, dataset, retriever, planner, *input, *output, *mode, *topK, *minScore, cfg)
}

// benchmarkEmbedder 按知识库绑定的模型选择嵌入配置，返回与正式索引一致的客户端。
func benchmarkEmbedder(ctx context.Context, cfg config.Config, dataset model.Dataset, repositories repo.Repositories) (*llm.Client, error) {
	settings := cfg.Embedding
	if dataset.EmbedModelID > 0 {
		configured, err := repositories.EmbedModels.GetByID(ctx, dataset.EmbedModelID)
		if err != nil {
			return nil, err
		}
		if configured.Name != model.OfficialEmbedName {
			settings.APIURL, settings.APIKey, settings.Model = configured.APIURL, configured.APIKey, configured.ModelID
		}
	}
	if settings.APIURL == "" || settings.Model == "" {
		return nil, fmt.Errorf("benchmark dataset embedding model is not configured")
	}
	return llm.New("", "", "", settings.APIURL, settings.APIKey, settings.Model), nil
}
