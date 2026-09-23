package repo

import (
	"gorm.io/gorm"
)

// Repositories 聚合数据访问实现。
type Repositories struct {
	DB           *gorm.DB
	Users        *UserRepository
	Datasets     *DatasetRepository
	Documents    *DocumentRepository
	Chunks       *ChunkRepository
	EmbedModels  *EmbedModelRepository
	VisionModels *VisionModelRepository
	Sessions     *SessionRepository
	Messages     *MessageRepository
	Outbox       *OutboxRepository
}

// NewRepositories 创建仓储集合。
func NewRepositories(db *gorm.DB) Repositories {
	return Repositories{
		DB:           db,
		Users:        NewUserRepository(db),
		Datasets:     NewDatasetRepository(db),
		Documents:    NewDocumentRepository(db),
		Chunks:       NewChunkRepository(db),
		EmbedModels:  NewEmbedModelRepository(db),
		VisionModels: NewVisionModelRepository(db),
		Sessions:     NewSessionRepository(db),
		Messages:     NewMessageRepository(db),
		Outbox:       NewOutboxRepository(db),
	}
}

// orEmpty 把 nil 切片换成空切片，避免调用方拿到 nil。
func orEmpty[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}
