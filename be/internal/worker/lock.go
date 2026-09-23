package worker

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"go.uber.org/zap"
)

type documentLocker interface {
	TryLock(ctx context.Context, documentID int64) (release func(), acquired bool, err error)
}

type postgresDocumentLocker struct {
	db *sql.DB
}

// TODO: advisory lock 会在整个文档解析周期独占一条 PostgreSQL 连接；后续并发提升时，
// 评估改用支持唯一持有者标识、自动续租和安全释放的 Redis 分布式锁。
// NewPostgresDocumentLocker 创建基于 PostgreSQL advisory lock 的文档锁。
func NewPostgresDocumentLocker(db *sql.DB) documentLocker {
	return &postgresDocumentLocker{db: db}
}

func (l *postgresDocumentLocker) TryLock(ctx context.Context, documentID int64) (func(), bool, error) {
	conn, err := l.db.Conn(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("获取文档锁连接失败: %w", err)
	}
	var acquired bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", documentID).Scan(&acquired); err != nil {
		_ = conn.Close()
		return nil, false, fmt.Errorf("获取文档锁失败: %w", err)
	}
	if !acquired {
		_ = conn.Close()
		return nil, false, nil
	}
	return func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var unlocked bool
		if err := conn.QueryRowContext(releaseCtx, "SELECT pg_advisory_unlock($1)", documentID).Scan(&unlocked); err != nil {
			zap.L().Error("释放文档锁失败", zap.Int64("documentId", documentID), zap.Error(err))
		}
		_ = conn.Close()
	}, true, nil
}
