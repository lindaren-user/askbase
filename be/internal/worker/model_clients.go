package worker

import (
	"context"
	"fmt"
	"strings"

	"askbase/be/internal/llm"
	"askbase/be/internal/model"

	"go.uber.org/zap"
)

// resolveEmbedder 用知识库绑定的嵌入模型构建客户端。
func (w *Worker) resolveEmbedder(ctx context.Context, ds model.Dataset) (embedder, error) {
	if ds.EmbedModelID <= 0 {
		if w.officialEmbed == nil {
			return nil, fmt.Errorf("%w: 服务端嵌入模型未配置", errPermanent)
		}
		zap.L().Info("使用服务端默认嵌入模型")
		return w.officialEmbed, nil
	}
	if w.embedModels == nil {
		return nil, fmt.Errorf("%w: 知识库未绑定嵌入模型", errPermanent)
	}
	configured, err := w.embedModels.GetByID(ctx, ds.EmbedModelID)
	if err != nil {
		return nil, fmt.Errorf("%w: 嵌入模型不存在", errPermanent)
	}
	if configured.Name == model.OfficialEmbedName {
		if w.officialEmbed == nil {
			return nil, fmt.Errorf("%w: 服务端嵌入模型未配置", errPermanent)
		}
		return w.officialEmbed, nil
	}
	if strings.TrimSpace(configured.APIURL) == "" || strings.TrimSpace(configured.ModelID) == "" {
		return nil, fmt.Errorf("%w: 嵌入模型配置不完整", errPermanent)
	}
	zap.L().Info("使用知识库嵌入模型", zap.String("model", configured.ModelID), zap.String("apiUrl", configured.APIURL))
	return llm.New("", "", "", configured.APIURL, configured.APIKey, configured.ModelID), nil
}

// resolveVision 按知识库绑定关系返回视觉模型；负 ID 表示显式禁用，零表示服务端默认。
func (w *Worker) resolveVision(ctx context.Context, ds model.Dataset) (imageDescriber, error) {
	if ds.VisionModelID < 0 {
		return nil, nil
	}
	if ds.VisionModelID == 0 {
		return w.officialVision, nil
	}
	if w.visionModels == nil {
		return nil, nil
	}
	configured, err := w.visionModels.GetByID(ctx, ds.VisionModelID)
	if err != nil {
		return nil, fmt.Errorf("%w: 视觉模型不存在", errPermanent)
	}
	if configured.Name == model.OfficialVisionName {
		return w.officialVision, nil
	}
	if strings.TrimSpace(configured.APIURL) == "" || strings.TrimSpace(configured.ModelID) == "" {
		return nil, fmt.Errorf("%w: 视觉模型配置不完整", errPermanent)
	}
	zap.L().Info("使用知识库视觉模型", zap.String("model", configured.ModelID), zap.String("apiUrl", configured.APIURL))
	return llm.New(configured.APIURL, configured.APIKey, configured.ModelID, "", "", ""), nil
}

// embedChunks 分批调用嵌入服务并回填向量；批间推进 60→95 进度。
func (w *Worker) embedChunks(ctx context.Context, emb embedder, ids []int64, texts []string, documentID int64, parseVersion int64) error {
	total := len(texts)
	for start := 0; start < total; start += w.batchSize {
		end := min(start+w.batchSize, total)
		vectors, err := emb.Embed(ctx, texts[start:end])
		if err != nil {
			return ioError(err, "批量嵌入失败")
		}
		if err := w.checkCancelled(ctx, documentID); err != nil {
			return err
		}
		for index, vector := range vectors {
			if err := w.chunks.UpdateVector(ctx, ids[start+index], vector); err != nil {
				return err
			}
		}
		progress := int16(60 + 35*end/total)
		if err := w.setStatus(ctx, documentID, parseVersion, model.DocumentStatusEmbedding, progress); err != nil {
			return err
		}
	}
	return nil
}
