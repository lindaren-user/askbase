package service

import (
	"context"
	"time"

	"askbase/be/internal/model"
)

// 下列接口由调用方持有，只列出该用例真正用到的方法。

type userStore interface {
	Create(ctx context.Context, email string, nickname string) (model.User, error)
	FindByID(ctx context.Context, id int64) (model.User, error)
	FindByEmail(ctx context.Context, email string) (model.User, error)
	Delete(ctx context.Context, id int64) error
}

type datasetByUserStore interface {
	GetByUser(ctx context.Context, userID int64, id int64) (model.Dataset, error)
}

type objectURLSigner interface {
	PresignGet(ctx context.Context, key string, filename string, contentType string, expires time.Duration) (string, error)
}

type objectDeleter interface {
	Delete(ctx context.Context, key string) error
}
