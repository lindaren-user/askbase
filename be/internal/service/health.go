package service

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// HealthService 健康检查。
type HealthService struct {
	db *gorm.DB
}

// NewHealthService 创建健康检查服务。
func NewHealthService(db *gorm.DB) *HealthService {
	return &HealthService{db: db}
}

// Check 探测数据库连接是否可用。
func (s *HealthService) Check(ctx context.Context) error {
	if s.db == nil {
		return errors.New("数据库未配置")
	}
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}
