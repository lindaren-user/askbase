// Package migration applies versioned PostgreSQL schema migrations.
package migration

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"

	"gorm.io/gorm"
)

var migrationNamePattern = regexp.MustCompile(`^(\d+)_.*\.up\.sql$`)

const advisoryLockID int64 = 0x41736B42617365

// File describes one ordered forward migration.
type File struct {
	Version int64
	Name    string
}

// Applied records a successfully committed migration.
type Applied struct {
	Version   int64     `gorm:"primaryKey"`
	Name      string    `gorm:"not null"`
	AppliedAt time.Time `gorm:"not null;autoCreateTime"`
}

// TableName specifies the migration history table.
func (Applied) TableName() string {
	return "t_schema_migration"
}

// Run applies all pending migrations in ascending version order.
func Run(ctx context.Context, db *gorm.DB, files fs.FS) error {
	migrations, err := discover(files)
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", advisoryLockID).Error; err != nil {
			return fmt.Errorf("获取迁移锁失败: %w", err)
		}
		if err := tx.AutoMigrate(&Applied{}); err != nil {
			return fmt.Errorf("创建迁移记录表失败: %w", err)
		}
		for _, migration := range migrations {
			if err := apply(tx, files, migration); err != nil {
				return err
			}
		}
		return nil
	})
}

// discover validates migration filenames and returns them in version order.
func discover(files fs.FS) ([]File, error) {
	names, err := fs.Glob(files, "*.up.sql")
	if err != nil {
		return nil, fmt.Errorf("扫描迁移文件失败: %w", err)
	}
	migrations := make([]File, 0, len(names))
	versions := make(map[int64]string, len(names))
	for _, name := range names {
		match := migrationNamePattern.FindStringSubmatch(filepath.Base(name))
		if match == nil {
			return nil, fmt.Errorf("迁移文件名无效: %s", name)
		}
		version, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("解析迁移版本失败 %s: %w", name, err)
		}
		if previous, exists := versions[version]; exists {
			return nil, fmt.Errorf("迁移版本重复: %s 与 %s", previous, name)
		}
		versions[version] = name
		migrations = append(migrations, File{Version: version, Name: name})
	}
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})
	return migrations, nil
}

// apply executes one pending migration and records it in the caller transaction.
func apply(tx *gorm.DB, files fs.FS, migration File) error {
	var count int64
	if err := tx.Model(&Applied{}).Where("version = ?", migration.Version).Count(&count).Error; err != nil {
		return fmt.Errorf("查询迁移状态失败 %s: %w", migration.Name, err)
	}
	if count > 0 {
		return nil
	}
	sql, err := fs.ReadFile(files, migration.Name)
	if err != nil {
		return fmt.Errorf("读取迁移文件失败 %s: %w", migration.Name, err)
	}
	if err := tx.Exec(string(sql)).Error; err != nil {
		return fmt.Errorf("执行迁移失败 %s: %w", migration.Name, err)
	}
	if err := tx.Create(&Applied{Version: migration.Version, Name: migration.Name}).Error; err != nil {
		return fmt.Errorf("记录迁移失败 %s: %w", migration.Name, err)
	}
	return nil
}
