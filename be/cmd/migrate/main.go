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
	"askbase/be/internal/migration"
	"askbase/be/migrations"

	"go.uber.org/zap"
)

// main connects to PostgreSQL and applies all pending schema migrations.
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	db, err := infra.NewPostgres(ctx, cfg.Postgres)
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

	if err := migration.Run(ctx, db, migrations.Files); err != nil {
		zap.L().Error("database migration failed", zap.Error(err))
		os.Exit(1)
	}
	zap.L().Info("database migration completed")
}
