package service

import (
	"context"
	"fmt"
	"strings"

	"askbase/be/internal/config"
	"askbase/be/internal/model"
	"askbase/be/internal/repo"
	"askbase/be/internal/tx"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

type datasetStore interface {
	Create(ctx context.Context, ds model.Dataset) (model.Dataset, error)
	GetByUser(ctx context.Context, userID int64, id int64) (model.Dataset, error)
	ListByUser(ctx context.Context, userID int64) ([]model.Dataset, error)
	Update(ctx context.Context, userID int64, id int64, name *string, description *string, chunkStrategy *string, visionModelID *int64, visionModel *string) error
	DeleteByUser(ctx context.Context, userID int64, id int64) error
}

// 级联删除所需的仓储能力。
type documentCascadeStore interface {
	ListR2KeysByDataset(ctx context.Context, datasetID int64) ([]string, error)
	DeleteByDataset(ctx context.Context, datasetID int64) ([]int64, error)
}

type chunkCascadeStore interface {
	DeleteByDataset(ctx context.Context, datasetID int64) error
	ListImageKeysByDataset(ctx context.Context, datasetID int64) ([]string, error)
}

type sessionCascadeStore interface {
	DeleteAllByUser(ctx context.Context, userID int64) error
}

type messageCascadeStore interface {
	DeleteAllByUser(ctx context.Context, userID int64) error
}

type datasetEmbedModelReader interface {
	GetByUser(ctx context.Context, userID int64, id int64) (model.EmbedModel, error)
}

type datasetVisionModelReader interface {
	GetByUser(ctx context.Context, userID int64, id int64) (model.VisionModel, error)
}

// DatasetService 知识库业务。
type DatasetService struct {
	db             *gorm.DB
	datasets       datasetStore
	documents      documentCascadeStore
	chunks         chunkCascadeStore
	sessions       sessionCascadeStore
	messages       messageCascadeStore
	objects        objectDeleter
	embedModels    datasetEmbedModelReader
	visionModels   datasetVisionModelReader
	embedOfficial  config.EmbeddingConfig
	visionOfficial config.VisionConfig
}

// NewDatasetService 创建知识库服务；embedModel 记录当前嵌入模型名。
func NewDatasetService(
	db *gorm.DB,
	datasets datasetStore,
	documents documentCascadeStore,
	chunks chunkCascadeStore,
	sessions sessionCascadeStore,
	messages messageCascadeStore,
	objects objectDeleter,
	embedModels datasetEmbedModelReader,
	visionModels datasetVisionModelReader,
	embedOfficial config.EmbeddingConfig,
	visionOfficial config.VisionConfig,
) *DatasetService {
	return &DatasetService{
		db:             db,
		datasets:       datasets,
		documents:      documents,
		chunks:         chunks,
		sessions:       sessions,
		messages:       messages,
		objects:        objects,
		embedModels:    embedModels,
		visionModels:   visionModels,
		embedOfficial:  embedOfficial,
		visionOfficial: visionOfficial,
	}
}

// Create 新建知识库，绑定解析模板与嵌入模型。
func (s *DatasetService) Create(ctx context.Context, userID int64, req model.CreateDatasetRequest) (model.Dataset, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return model.Dataset{}, fmt.Errorf("%w: 知识库名称不能为空", ErrInvalidInput)
	}
	if len([]rune(name)) > 128 {
		return model.Dataset{}, fmt.Errorf("%w: 知识库名称过长", ErrInvalidInput)
	}
	strategy := strings.TrimSpace(req.ChunkStrategy)
	if strategy == "" {
		strategy = model.ChunkStrategyGeneral
	}
	if err := validateChunkStrategy(strategy); err != nil {
		return model.Dataset{}, err
	}
	embedID, embedName, err := s.resolveEmbedBinding(ctx, userID, req.EmbedModelID)
	if err != nil {
		return model.Dataset{}, err
	}
	visionID, visionName, err := s.resolveVisionBinding(ctx, userID, req.VisionModelID)
	if err != nil {
		return model.Dataset{}, err
	}
	ds, err := s.datasets.Create(ctx, model.Dataset{
		UserID:        userID,
		Name:          name,
		Description:   strings.TrimSpace(req.Description),
		ChunkStrategy: strategy,
		EmbedModel:    embedName,
		EmbedModelID:  embedID,
		VisionModel:   visionName,
		VisionModelID: visionID,
	})
	if err != nil {
		return model.Dataset{}, err
	}
	return ds, nil
}

// List 列出当前用户知识库（含文档/分块聚合计数）。
func (s *DatasetService) List(ctx context.Context, userID int64) ([]model.Dataset, error) {
	return s.datasets.ListByUser(ctx, userID)
}

// Get 查询单个知识库。
func (s *DatasetService) Get(ctx context.Context, userID int64, id int64) (model.Dataset, error) {
	ds, err := s.datasets.GetByUser(ctx, userID, id)
	if err != nil {
		return model.Dataset{}, asNotFound(err, repo.ErrDatasetNotFound)
	}
	return ds, nil
}

// Update 更新知识库名称、描述与视觉模型；解析模板与嵌入模型创建后不可更换。
func (s *DatasetService) Update(ctx context.Context, userID int64, id int64, req model.UpdateDatasetRequest) error {
	if _, err := s.Get(ctx, userID, id); err != nil {
		return err
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return fmt.Errorf("%w: 知识库名称不能为空", ErrInvalidInput)
		}
		if len([]rune(name)) > 128 {
			return fmt.Errorf("%w: 知识库名称过长", ErrInvalidInput)
		}
		req.Name = &name
	}
	var visionName *string
	if req.VisionModelID != nil {
		id, name, err := s.resolveVisionBinding(ctx, userID, *req.VisionModelID)
		if err != nil {
			return err
		}
		req.VisionModelID = &id
		visionName = &name
	}
	err := s.datasets.Update(ctx, userID, id, req.Name, req.Description, nil, req.VisionModelID, visionName)
	return asNotFound(err, repo.ErrDatasetNotFound)
}

func (s *DatasetService) resolveEmbedBinding(ctx context.Context, userID, id int64) (int64, string, error) {
	if id == model.OfficialModelID {
		if strings.TrimSpace(s.embedOfficial.APIURL) == "" || strings.TrimSpace(s.embedOfficial.Model) == "" {
			return 0, "", fmt.Errorf("%w: 服务端嵌入模型未配置", ErrInvalidInput)
		}
		return model.OfficialModelID, model.OfficialEmbedName, nil
	}
	if id <= 0 {
		return 0, "", fmt.Errorf("%w: 请选择嵌入模型", ErrInvalidInput)
	}
	emb, err := s.embedModels.GetByUser(ctx, userID, id)
	if err != nil {
		return 0, "", asNotFound(err, repo.ErrEmbedModelNotFound)
	}
	return emb.ID, emb.Name, nil
}

func (s *DatasetService) resolveVisionBinding(ctx context.Context, userID, id int64) (int64, string, error) {
	if id < 0 {
		return model.UnusedVisionModelID, "", nil
	}
	if id == model.OfficialModelID {
		if strings.TrimSpace(s.visionOfficial.APIURL) == "" || strings.TrimSpace(s.visionOfficial.Model) == "" {
			return model.UnusedVisionModelID, "", nil
		}
		return model.OfficialModelID, model.OfficialVisionName, nil
	}
	vm, err := s.visionModels.GetByUser(ctx, userID, id)
	if err != nil {
		return 0, "", asNotFound(err, repo.ErrVisionModelNotFound)
	}
	return vm.ID, vm.Name, nil
}

func validateChunkStrategy(strategy string) error {
	if model.IsChunkStrategy(strategy) {
		return nil
	}
	return fmt.Errorf("%w: 不支持的分块策略", ErrInvalidInput)
}

// Delete 删除知识库并级联清理文档与分块；会话保留，前端标记所属知识库已删除。
func (s *DatasetService) Delete(ctx context.Context, userID int64, id int64) error {
	if _, err := s.Get(ctx, userID, id); err != nil {
		return err
	}

	// 对象存储清理放在事务外：失败不回滚 DB，残留对象可容忍（后台可再清）
	keys, err := s.documents.ListR2KeysByDataset(ctx, id)
	if err != nil {
		zap.L().Warn("列出知识库对象失败，跳过对象清理", zap.Int64("datasetId", id), zap.Error(err))
	} else {
		deleteObjectKeys(ctx, s.objects, keys)
	}
	if imgKeys, err := s.chunks.ListImageKeysByDataset(ctx, id); err != nil {
		zap.L().Warn("列出知识库分块图片失败", zap.Int64("datasetId", id), zap.Error(err))
	} else {
		deleteObjectKeys(ctx, s.objects, imgKeys)
	}

	return tx.With(ctx, s.db, func(ctx context.Context) error {
		if err := s.chunks.DeleteByDataset(ctx, id); err != nil {
			return fmt.Errorf("删除知识库分块失败: %w", err)
		}
		if _, err := s.documents.DeleteByDataset(ctx, id); err != nil {
			return fmt.Errorf("删除知识库文档失败: %w", err)
		}
		return asNotFound(s.datasets.DeleteByUser(ctx, userID, id), repo.ErrDatasetNotFound)
	})
}

// DeleteUserConversations 注销账号时删除该用户全部消息与会话。
func (s *DatasetService) DeleteUserConversations(ctx context.Context, userID int64) error {
	if err := s.messages.DeleteAllByUser(ctx, userID); err != nil {
		return fmt.Errorf("删除用户消息失败: %w", err)
	}
	if err := s.sessions.DeleteAllByUser(ctx, userID); err != nil {
		return fmt.Errorf("删除用户会话失败: %w", err)
	}
	return nil
}
