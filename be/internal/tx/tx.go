// Package tx 在 context 中传递事务会话，repo 层据此复用事务或退回普通连接。
package tx

import (
	"context"

	"gorm.io/gorm"
)

type ctxKey struct{}

// Manager 提供应用层事务边界，不向业务代码暴露 GORM 事务对象。
type Manager struct {
	db *gorm.DB
}

// NewManager 创建事务管理器。
func NewManager(db *gorm.DB) *Manager {
	return &Manager{db: db}
}

// Within 在 fn 内以事务执行；fn 返回错误则回滚。
func (m *Manager) Within(ctx context.Context, fn func(ctx context.Context) error) error {
	return m.db.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		return fn(context.WithValue(ctx, ctxKey{}, transaction))
	})
}

// With 在 fn 内以事务执行；fn 返回错误则回滚。
func With(ctx context.Context, db *gorm.DB, fn func(ctx context.Context) error) error {
	return NewManager(db).Within(ctx, fn)
}

// DB 取当前上下文中的数据库会话；若已在事务中则复用该事务。
func DB(ctx context.Context, db *gorm.DB) *gorm.DB {
	if tx, ok := ctx.Value(ctxKey{}).(*gorm.DB); ok && tx != nil {
		return tx
	}
	return db.WithContext(ctx)
}
