package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"askbase/be/internal/config"
	"askbase/be/internal/infra"
	"askbase/be/internal/llm"
	"askbase/be/internal/logging"
	"askbase/be/internal/queue"
	"askbase/be/internal/repo"
	"askbase/be/internal/storage"
	"askbase/be/internal/worker"

	"go.uber.org/zap"
)

// main 装配 RabbitMQ 消费者与文档解析流水线。
func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Printf("load config failed: %v", err)
		os.Exit(1)
	}
	if err := logging.Init(cfg.Log); err != nil {
		log.Printf("init logger failed: %v", err)
		os.Exit(1)
	}
	defer func() { _ = zap.L().Sync() }()

	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startupCtx, cancelStartup := context.WithTimeout(runCtx, 10*time.Second)
	defer cancelStartup()

	db, err := infra.NewPostgres(startupCtx, cfg.Postgres)
	if err != nil {
		zap.L().Error("connect postgres failed", zap.Error(err))
		os.Exit(1)
	}
	sqlDB, err := db.DB()
	if err != nil {
		zap.L().Error("get postgres pool failed", zap.Error(err))
		os.Exit(1)
	}
	defer sqlDB.Close()

	rabbit, err := infra.NewRabbitMQ(startupCtx, cfg.RabbitMQ)
	if err != nil {
		zap.L().Error("connect rabbitmq failed", zap.Error(err))
		os.Exit(1)
	}
	defer rabbit.Close()
	publisher, err := queue.NewPublisher(rabbit, cfg.Queue)
	if err != nil {
		zap.L().Error("init rabbitmq publisher failed", zap.Error(err))
		os.Exit(1)
	}
	defer publisher.Close()
	consumer, err := queue.NewConsumer(rabbit, publisher, cfg.Queue)
	if err != nil {
		zap.L().Error("init rabbitmq consumer failed", zap.Error(err))
		os.Exit(1)
	}
	defer consumer.Close()

	storageClient, err := storage.NewClient(cfg.Storage)
	if err != nil {
		zap.L().Error("init storage client failed", zap.Error(err))
		os.Exit(1)
	}
	llmClient := llm.New(
		cfg.LLM.APIURL,
		cfg.LLM.APIKey,
		cfg.LLM.Model,
		cfg.Embedding.APIURL,
		cfg.Embedding.APIKey,
		cfg.Embedding.Model,
	)
	var officialVision *llm.Client
	if cfg.Vision.APIURL != "" && cfg.Vision.Model != "" {
		officialVision = llm.New(cfg.Vision.APIURL, cfg.Vision.APIKey, cfg.Vision.Model, "", "", "")
	}
	repositories := repo.NewRepositories(db)
	pipeline := worker.New(
		repositories.Documents,
		repositories.Datasets,
		repositories.EmbedModels,
		repositories.VisionModels,
		repositories.Chunks,
		storageClient,
		cfg.Embedding.BatchSize,
		llmClient,
		officialVision,
		cfg.MinerU,
		worker.NewPostgresDocumentLocker(sqlDB),
	)

	zap.L().Info("document worker started", zap.Int("concurrency", cfg.Queue.Concurrency))
	if err := consumer.Run(runCtx, pipeline.HandleParse, pipeline.HandleTerminal); err != nil {
		zap.L().Error("document worker stopped", zap.Error(err))
		os.Exit(1)
	}
}
