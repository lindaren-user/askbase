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
	"askbase/be/internal/logging"
	"askbase/be/internal/outbox"
	"askbase/be/internal/queue"
	"askbase/be/internal/repo"
	"askbase/be/internal/tx"

	"go.uber.org/zap"
)

// main 装配 PostgreSQL Outbox relay 与 RabbitMQ publisher。
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

	repositories := repo.NewRepositories(db)
	relay := outbox.NewRelay(tx.NewManager(db), repositories.Outbox, publisher, cfg.Outbox)
	zap.L().Info("outbox relay started")
	if err := relay.Run(runCtx); err != nil {
		zap.L().Error("outbox relay stopped", zap.Error(err))
		os.Exit(1)
	}
}
