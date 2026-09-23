package repo

import (
	"context"
	"errors"
	"fmt"

	"askbase/be/internal/model"
	"askbase/be/internal/tx"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// UserRepository 用户数据访问。
type UserRepository struct {
	db *gorm.DB
}

// NewUserRepository 创建用户仓储。
func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

// conn 取当前上下文中的数据库会话；若已在事务中则复用该事务。
func conn(ctx context.Context, db *gorm.DB) *gorm.DB {
	return tx.DB(ctx, db)
}

// Create 创建用户。
func (r *UserRepository) Create(ctx context.Context, email string, nickname string) (model.User, error) {
	user := model.User{
		Email:    email,
		Nickname: nickname,
		Status:   model.UserStatusNormal,
	}
	if err := conn(ctx, r.db).Create(&user).Error; err != nil {
		if isUniqueViolation(err, "t_user_email_key") {
			return model.User{}, ErrEmailExists
		}
		return model.User{}, fmt.Errorf("创建用户失败: %w", err)
	}
	return user, nil
}

// FindByID 按 ID 查询正常状态用户。
func (r *UserRepository) FindByID(ctx context.Context, id int64) (model.User, error) {
	return r.findActive(ctx, "id = ?", id)
}

// FindByEmail 按邮箱查询正常状态用户。
func (r *UserRepository) FindByEmail(ctx context.Context, email string) (model.User, error) {
	return r.findActive(ctx, "email = ?", email)
}

// ListIDs 列出全部用户 ID。
func (r *UserRepository) ListIDs(ctx context.Context) ([]int64, error) {
	var ids []int64
	if err := conn(ctx, r.db).Model(&model.User{}).Pluck("id", &ids).Error; err != nil {
		return nil, fmt.Errorf("查询用户 ID 失败: %w", err)
	}
	return ids, nil
}

// Delete 物理删除用户；业务数据级联清理由服务层编排。
func (r *UserRepository) Delete(ctx context.Context, id int64) error {
	res := conn(ctx, r.db).Where("id = ?", id).Delete(&model.User{})
	if res.Error != nil {
		return fmt.Errorf("删除用户失败: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrUserNotFound
	}
	return nil
}

// findActive 按条件查询状态正常的用户；不存在时返回 ErrUserNotFound。
func (r *UserRepository) findActive(ctx context.Context, query string, arg any) (model.User, error) {
	var user model.User
	err := conn(ctx, r.db).
		Where(query, arg).
		Where("status = ?", model.UserStatusNormal).
		Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.User{}, ErrUserNotFound
	}
	if err != nil {
		return model.User{}, fmt.Errorf("查询用户失败: %w", err)
	}
	return user, nil
}

// isUniqueViolation 判断错误是否为指定唯一约束冲突；constraint 为空时只看唯一冲突码。
func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" {
			return constraint == "" || pgErr.ConstraintName == constraint
		}
	}
	// GORM TranslateError 模式下唯一冲突被翻译为 ErrDuplicatedKey
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	return false
}
