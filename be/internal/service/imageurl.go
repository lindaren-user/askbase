package service

import (
	"context"
	"path"
	"strings"
	"time"

	"askbase/be/internal/model"
	"go.uber.org/zap"
)

func attachChunkImageURLs(ctx context.Context, objects objectURLSigner, chunks []model.Chunk) {
	for i := range chunks {
		chunks[i].ImageURL = presignImageURL(ctx, objects, chunks[i].ImageKey)
	}
}

func attachHitImageURLs(ctx context.Context, objects objectURLSigner, hits []model.RetrievalHit) {
	for i := range hits {
		hits[i].ImageURL = presignImageURL(ctx, objects, hits[i].ImageKey)
	}
}

func presignImageURL(ctx context.Context, objects objectURLSigner, key string) string {
	if objects == nil || strings.TrimSpace(key) == "" {
		return ""
	}
	url, err := objects.PresignGet(ctx, key, "", imageContentType(key), 10*time.Minute)
	if err != nil {
		zap.L().Warn("生成图片预签名失败", zap.String("key", key), zap.Error(err))
		return ""
	}
	return url
}

func imageContentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".tif", ".tiff":
		return "image/tiff"
	default:
		return "image/png"
	}
}

func deleteObjectKeys(ctx context.Context, objects objectDeleter, keys []string) {
	if objects == nil {
		return
	}
	for _, key := range keys {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if err := objects.Delete(ctx, key); err != nil {
			zap.L().Warn("删除对象失败", zap.String("key", key), zap.Error(err))
		}
	}
}
