package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"askbase/be/internal/config"
	"askbase/be/internal/infra"
	"askbase/be/internal/llm"
	"askbase/be/internal/logging"
	"askbase/be/internal/mail"
	"askbase/be/internal/repo"
	"askbase/be/internal/router"
	"askbase/be/internal/service"
	"askbase/be/internal/storage"

	"go.uber.org/zap"
)

// main 装配配置、数据库、仓储、服务与路由并启动 HTTP API。
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

	mailSender, err := mail.NewSender(cfg.Mail)
	if err != nil {
		zap.L().Error("init mail sender failed", zap.Error(err))
		os.Exit(1)
	}

	storageClient, err := storage.NewClient(cfg.Storage)
	if err != nil {
		zap.L().Error("init storage client failed", zap.Error(err))
		os.Exit(1)
	}

	// OpenAI 兼容客户端：对话与嵌入可指向不同服务
	llmClient := llm.New(
		cfg.LLM.APIURL, cfg.LLM.APIKey, cfg.LLM.Model,
		cfg.Embedding.APIURL, cfg.Embedding.APIKey, cfg.Embedding.Model,
	)

	repositories := repo.NewRepositories(db)

	services, err := service.NewServices(repositories, cfg, mailSender, storageClient, llmClient)
	if err != nil {
		zap.L().Error("init services failed", zap.Error(err))
		os.Exit(1)
	}
	httpHandler := router.New(services, cfg.Env)

	listenAddr := cfg.HTTP.ListenAddr()
	server := &http.Server{
		Addr:              listenAddr,
		Handler:           httpHandler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       5 * time.Minute,
	}

	zap.L().Info("server started", zap.String("addr", listenAddr))
	serverErr := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stop)

	select {
	case err := <-serverErr:
		if err != nil {
			zap.L().Error("server stopped", zap.Error(err))
			os.Exit(1)
		}
		return
	case sig := <-stop:
		zap.L().Info("shutdown signal received", zap.String("signal", sig.String()))
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		zap.L().Error("server shutdown failed", zap.Error(err))
		os.Exit(1)
	}
}
