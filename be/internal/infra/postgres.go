package infra

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"askbase/be/internal/config"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// NewPostgres 创建 GORM 连接并探测可用性。表结构由迁移脚本维护，不自动建表。
func NewPostgres(ctx context.Context, cfg config.PostgresConfig) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(postgresDSN(cfg)), &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Silent),
		SkipDefaultTransaction: true,
		TranslateError:         true,
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("获取数据库连接池失败: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	if cfg.ConnMaxLifetime != "" {
		lifetime, err := time.ParseDuration(cfg.ConnMaxLifetime)
		if err != nil {
			_ = sqlDB.Close()
			return nil, err
		}
		sqlDB.SetConnMaxLifetime(lifetime)
	}

	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

func postgresDSN(cfg config.PostgresConfig) string {
	values := url.Values{}
	values.Set("sslmode", cfg.SSLMode)

	dsn := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(cfg.User, cfg.Password),
		Host:     cfg.Host + ":" + strconv.Itoa(cfg.Port),
		Path:     cfg.Database,
		RawQuery: values.Encode(),
	}
	return dsn.String()
}
