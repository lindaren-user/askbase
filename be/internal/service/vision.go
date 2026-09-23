package service

import (
	"context"
	"fmt"
	"strings"

	"askbase/be/internal/config"
	"askbase/be/internal/model"
	"askbase/be/internal/repo"
)

type visionModelStore interface {
	Create(ctx context.Context, m model.VisionModel) (model.VisionModel, error)
	GetByUser(ctx context.Context, userID int64, id int64) (model.VisionModel, error)
	ListByUser(ctx context.Context, userID int64) ([]model.VisionModel, error)
	Update(ctx context.Context, userID int64, id int64, updates map[string]any) error
	DeleteByUser(ctx context.Context, userID int64, id int64) error
}

type visionDatasetStore interface {
	CountByVisionModel(ctx context.Context, userID int64, visionModelID int64) (int64, error)
}

// VisionModelService 用户视觉模型 CRUD。
type VisionModelService struct {
	models   visionModelStore
	datasets visionDatasetStore
	official config.VisionConfig
}

// NewVisionModelService 创建服务。
func NewVisionModelService(models visionModelStore, datasets visionDatasetStore, official config.VisionConfig) *VisionModelService {
	return &VisionModelService{models: models, datasets: datasets, official: official}
}

func officialVisionView(cfg config.VisionConfig) (model.VisionModelView, bool) {
	if strings.TrimSpace(cfg.APIURL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return model.VisionModelView{}, false
	}
	return model.VisionModel{ID: model.OfficialModelID, Name: model.OfficialVisionName, ModelID: cfg.Model, APIURL: cfg.APIURL, APIKey: cfg.APIKey}.ToView(), true
}

// List 列出服务端默认（来自 config）及用户自己登记的视觉模型。
func (s *VisionModelService) List(ctx context.Context, userID int64) ([]model.VisionModelView, error) {
	items, err := s.models.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]model.VisionModelView, 0, len(items)+1)
	if v, ok := officialVisionView(s.official); ok {
		out = append(out, v)
	}
	for _, m := range items {
		if m.Name == model.OfficialVisionName {
			continue
		}
		out = append(out, m.ToView())
	}
	return out, nil
}

// Create 登记视觉模型。
func (s *VisionModelService) Create(ctx context.Context, userID int64, req model.CreateVisionModelRequest) (model.VisionModelView, error) {
	m, err := normalizeVisionInput(req.Name, req.ModelID, req.APIURL, req.APIKey)
	if err != nil {
		return model.VisionModelView{}, err
	}
	if m.Name == model.OfficialVisionName {
		return model.VisionModelView{}, fmt.Errorf("%w: 该名称已保留给服务端默认模型", ErrInvalidInput)
	}
	m.UserID = userID
	created, err := s.models.Create(ctx, m)
	if err != nil {
		return model.VisionModelView{}, err
	}
	return created.ToView(), nil
}

// Update 更新视觉模型。
func (s *VisionModelService) Update(ctx context.Context, userID int64, id int64, req model.UpdateVisionModelRequest) error {
	if id <= 0 {
		return fmt.Errorf("%w: 服务端默认模型不可修改", ErrConflict)
	}
	if _, err := s.models.GetByUser(ctx, userID, id); err != nil {
		return asNotFound(err, repo.ErrVisionModelNotFound)
	}
	updates := map[string]any{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return fmt.Errorf("%w: 请填写模型名称", ErrInvalidInput)
		}
		if name == model.OfficialVisionName {
			return fmt.Errorf("%w: 该名称已保留给服务端默认模型", ErrInvalidInput)
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
	return asNotFound(err, repo.ErrVisionModelNotFound)
}

// Delete 删除未被知识库引用的视觉模型。
func (s *VisionModelService) Delete(ctx context.Context, userID int64, id int64) error {
	if id <= 0 {
		return fmt.Errorf("%w: 服务端默认模型不可删除", ErrConflict)
	}
	m, err := s.models.GetByUser(ctx, userID, id)
	if err != nil {
		return asNotFound(err, repo.ErrVisionModelNotFound)
	}
	if m.Name == model.OfficialVisionName {
		return fmt.Errorf("%w: 服务端默认模型不可删除", ErrConflict)
	}
	n, err := s.datasets.CountByVisionModel(ctx, userID, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("%w: 仍有知识库使用该视觉模型，无法删除", ErrConflict)
	}
	return asNotFound(s.models.DeleteByUser(ctx, userID, id), repo.ErrVisionModelNotFound)
}

func normalizeVisionInput(name, modelID, apiURL, apiKey string) (model.VisionModel, error) {
	modelID = strings.TrimSpace(modelID)
	apiURL = strings.TrimSpace(apiURL)
	name = strings.TrimSpace(name)
	if modelID == "" || apiURL == "" {
		return model.VisionModel{}, fmt.Errorf("%w: 请填写模型 ID 与接口前缀", ErrInvalidInput)
	}
	if name == "" {
		name = modelID
	}
	return model.VisionModel{
		Name:    name,
		ModelID: modelID,
		APIURL:  apiURL,
		APIKey:  strings.TrimSpace(apiKey),
	}, nil
}
