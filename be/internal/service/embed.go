package service

import (
	"context"
	"fmt"
	"strings"

	"askbase/be/internal/config"
	"askbase/be/internal/llm"
	"askbase/be/internal/model"
	"askbase/be/internal/repo"
)

type embedModelStore interface {
	Create(ctx context.Context, m model.EmbedModel) (model.EmbedModel, error)
	GetByUser(ctx context.Context, userID int64, id int64) (model.EmbedModel, error)
	ListByUser(ctx context.Context, userID int64) ([]model.EmbedModel, error)
	Update(ctx context.Context, userID int64, id int64, updates map[string]any) error
	DeleteByUser(ctx context.Context, userID int64, id int64) error
}

type embedDatasetStore interface {
	CountByEmbedModel(ctx context.Context, userID int64, embedModelID int64) (int64, error)
}

type embedModelReader interface {
	GetByID(ctx context.Context, id int64) (model.EmbedModel, error)
}

// EmbedModelService 用户嵌入模型 CRUD。
type EmbedModelService struct {
	models   embedModelStore
	datasets embedDatasetStore
	official config.EmbeddingConfig
}

// NewEmbedModelService 创建服务。
func NewEmbedModelService(models embedModelStore, datasets embedDatasetStore, official config.EmbeddingConfig) *EmbedModelService {
	return &EmbedModelService{models: models, datasets: datasets, official: official}
}

func officialEmbedView(cfg config.EmbeddingConfig) (model.EmbedModelView, bool) {
	if strings.TrimSpace(cfg.APIURL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return model.EmbedModelView{}, false
	}
	return model.EmbedModelView{
		ID:           model.OfficialModelID,
		Name:         model.OfficialEmbedName,
		ModelID:      cfg.Model,
		APIURL:       cfg.APIURL,
		APIKeyMasked: maskOfficialKey(cfg.APIKey),
	}, true
}

func maskOfficialKey(key string) string {
	return model.EmbedModel{APIKey: key}.ToView().APIKeyMasked
}

// List 列出服务端默认（来自 config）及用户自己登记的嵌入模型。
func (s *EmbedModelService) List(ctx context.Context, userID int64) ([]model.EmbedModelView, error) {
	items, err := s.models.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]model.EmbedModelView, 0, len(items)+1)
	if v, ok := officialEmbedView(s.official); ok {
		out = append(out, v)
	}
	for _, m := range items {
		if m.Name == model.OfficialEmbedName {
			continue
		}
		out = append(out, m.ToView())
	}
	return out, nil
}

// Create 登记嵌入模型。
func (s *EmbedModelService) Create(ctx context.Context, userID int64, req model.CreateEmbedModelRequest) (model.EmbedModelView, error) {
	m, err := normalizeEmbedInput(req.Name, req.ModelID, req.APIURL, req.APIKey)
	if err != nil {
		return model.EmbedModelView{}, err
	}
	if m.Name == model.OfficialEmbedName {
		return model.EmbedModelView{}, fmt.Errorf("%w: 该名称已保留给服务端默认模型", ErrInvalidInput)
	}
	m.UserID = userID
	created, err := s.models.Create(ctx, m)
	if err != nil {
		return model.EmbedModelView{}, err
	}
	return created.ToView(), nil
}

// Update 更新嵌入模型。
func (s *EmbedModelService) Update(ctx context.Context, userID int64, id int64, req model.UpdateEmbedModelRequest) error {
	if id <= 0 {
		return fmt.Errorf("%w: 服务端默认模型不可修改", ErrConflict)
	}
	if _, err := s.models.GetByUser(ctx, userID, id); err != nil {
		return asNotFound(err, repo.ErrEmbedModelNotFound)
	}
	updates := map[string]any{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return fmt.Errorf("%w: 请填写模型名称", ErrInvalidInput)
		}
		updates["name"] = name
	}
	if req.ModelID != nil {
		idStr := strings.TrimSpace(*req.ModelID)
		if idStr == "" {
			return fmt.Errorf("%w: 请填写模型 ID", ErrInvalidInput)
		}
		updates["model_id"] = idStr
	}
	if req.APIURL != nil {
		url := strings.TrimSpace(*req.APIURL)
		if url == "" {
			return fmt.Errorf("%w: 请填写接口前缀", ErrInvalidInput)
		}
		updates["api_url"] = url
	}
	if req.APIKey != nil {
		updates["api_key"] = strings.TrimSpace(*req.APIKey)
	}
	err := s.models.Update(ctx, userID, id, updates)
	return asNotFound(err, repo.ErrEmbedModelNotFound)
}

// Delete 删除未被知识库引用的嵌入模型。
func (s *EmbedModelService) Delete(ctx context.Context, userID int64, id int64) error {
	if id <= 0 {
		return fmt.Errorf("%w: 服务端默认模型不可删除", ErrConflict)
	}
	m, err := s.models.GetByUser(ctx, userID, id)
	if err != nil {
		return asNotFound(err, repo.ErrEmbedModelNotFound)
	}
	if m.Name == model.OfficialEmbedName {
		return fmt.Errorf("%w: 服务端默认模型不可删除", ErrConflict)
	}
	n, err := s.datasets.CountByEmbedModel(ctx, userID, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("%w: 仍有知识库使用该嵌入模型，无法删除", ErrConflict)
	}
	return asNotFound(s.models.DeleteByUser(ctx, userID, id), repo.ErrEmbedModelNotFound)
}

func normalizeEmbedInput(name, modelID, apiURL, apiKey string) (model.EmbedModel, error) {
	modelID = strings.TrimSpace(modelID)
	apiURL = strings.TrimSpace(apiURL)
	name = strings.TrimSpace(name)
	if modelID == "" || apiURL == "" {
		return model.EmbedModel{}, fmt.Errorf("%w: 请填写模型 ID 与接口前缀", ErrInvalidInput)
	}
	if name == "" {
		name = modelID
	}
	return model.EmbedModel{
		Name:    name,
		ModelID: modelID,
		APIURL:  apiURL,
		APIKey:  strings.TrimSpace(apiKey),
	}, nil
}

func embedderFrom(m model.EmbedModel) *llm.Client {
	return llm.New("", "", "", m.APIURL, m.APIKey, m.ModelID)
}

func loadDatasetEmbedder(ctx context.Context, models embedModelReader, ds model.Dataset, official config.EmbeddingConfig) (*llm.Client, error) {
	if ds.EmbedModelID <= 0 {
		if strings.TrimSpace(official.APIURL) == "" || strings.TrimSpace(official.Model) == "" {
			return nil, fmt.Errorf("%w: 服务端嵌入模型未配置", ErrInvalidInput)
		}
		return embedderFrom(model.EmbedModel{APIURL: official.APIURL, APIKey: official.APIKey, ModelID: official.Model}), nil
	}
	m, err := models.GetByID(ctx, ds.EmbedModelID)
	if err != nil {
		return nil, asNotFound(err, repo.ErrEmbedModelNotFound)
	}
	if m.Name == model.OfficialEmbedName {
		if strings.TrimSpace(official.APIURL) == "" || strings.TrimSpace(official.Model) == "" {
			return nil, fmt.Errorf("%w: 服务端嵌入模型未配置", ErrInvalidInput)
		}
		return embedderFrom(model.EmbedModel{APIURL: official.APIURL, APIKey: official.APIKey, ModelID: official.Model}), nil
	}
	if strings.TrimSpace(m.APIURL) == "" || strings.TrimSpace(m.ModelID) == "" {
		return nil, fmt.Errorf("%w: 嵌入模型配置不完整", ErrInvalidInput)
	}
	return embedderFrom(m), nil
}
