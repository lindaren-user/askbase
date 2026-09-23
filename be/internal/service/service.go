package service

import (
	"askbase/be/internal/config"
	"askbase/be/internal/llm"
	"askbase/be/internal/mail"
	"askbase/be/internal/repo"
	"askbase/be/internal/storage"
)

// Services 聚合业务服务。
type Services struct {
	Health       *HealthService
	Auth         *AuthService
	Datasets     *DatasetService
	Documents    *DocumentService
	Chunks       *ChunkService
	EmbedModels  *EmbedModelService
	VisionModels *VisionModelService
	Retrieval    *RetrievalService
	Sessions     *SessionService
	Chat         *ChatService
	Models       *ModelService
}

// NewServices 组装业务服务。验证码放在进程内 Ristretto，1 分钟过期。
func NewServices(
	r repo.Repositories,
	cfg config.Config,
	mailSender mail.Sender,
	storageClient *storage.S3Client,
	llmClient *llm.Client,
) (Services, error) {
	datasets := NewDatasetService(r.DB, r.Datasets, r.Documents, r.Chunks, r.Sessions, r.Messages, storageClient, r.EmbedModels, r.VisionModels, cfg.Embedding, cfg.Vision)
	auth, err := NewAuthService(r.DB, r.Users, datasets, r.EmbedModels, r.VisionModels, cfg.Auth, cfg.Env, mailSender)
	if err != nil {
		return Services{}, err
	}
	embedModels := NewEmbedModelService(r.EmbedModels, r.Datasets, cfg.Embedding)
	visionModels := NewVisionModelService(r.VisionModels, r.Datasets, cfg.Vision)
	retrieval := NewRetrievalService(r.Datasets, r.EmbedModels, r.Chunks, storageClient, cfg.Chat.Retrieval, cfg.Embedding)
	queryRouter := NewLLMQueryRouter(llmClient, cfg.Chat.Router)
	queryPlanner := NewQueryPlanner(queryRouter, cfg.Chat.Router.MaxSubqueries)
	return Services{
		Health:   NewHealthService(r.DB),
		Auth:     auth,
		Datasets: datasets,
		Documents: NewDocumentService(
			r.DB, r.Datasets, r.Documents, r.Chunks,
			storageClient, cfg.Storage.S3.Prefix, r.Outbox,
		),
		Chunks:       NewChunkService(r.Chunks, r.Documents, r.Datasets, r.EmbedModels, storageClient, cfg.Embedding),
		EmbedModels:  embedModels,
		VisionModels: visionModels,
		Retrieval:    retrieval,
		Sessions:     NewSessionService(r.Sessions, r.Messages, r.Datasets, r.Chunks, r.Documents, storageClient),
		Chat:         NewChatService(r.Sessions, r.Messages, r.Datasets, retrieval, queryPlanner, llmClient),
		Models:       NewModelService(),
	}, nil
}
