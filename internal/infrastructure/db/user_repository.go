package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/model"
)

// 编译期断言：基础设施层 UserRepository 满足服务层定义的仓储接口。

var _ service.UserRepository = (*UserRepository)(nil)
var _ service.MembershipUserRepository = (*UserRepository)(nil)

type UserRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(ctx context.Context, user *model.User) error {
	if err := r.db.WithContext(ctx).Create(userToRow(user)).Error; err != nil {
		return translateDBError(err, "创建用户失败")
	}
	return nil
}

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*model.User, error) {
	var row UserRow
	if err := r.db.WithContext(ctx).First(&row, "email = ?", strings.ToLower(strings.TrimSpace(email))).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRepositoryNotFound
		}
		return nil, fmt.Errorf("按邮箱读取用户失败: %w", err)
	}
	return userFromRow(&row), nil
}

func (r *UserRepository) FindByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	var row UserRow
	if err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRepositoryNotFound
		}
		return nil, fmt.Errorf("读取用户失败: %w", err)
	}
	return userFromRow(&row), nil
}

// ListByIDs 批量读取用户；不存在的 ID 不产生结果。
func (r *UserRepository) ListByIDs(ctx context.Context, ids []uuid.UUID) ([]*model.User, error) {
	if len(ids) == 0 {
		return []*model.User{}, nil
	}
	var rows []UserRow
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("批量读取用户失败: %w", err)
	}
	result := make([]*model.User, 0, len(rows))
	for i := range rows {
		result = append(result, userFromRow(&rows[i]))
	}
	return result, nil
}

func (r *UserRepository) Count(ctx context.Context) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&UserRow{}).Count(&count).Error; err != nil {
		return 0, fmt.Errorf("统计用户失败: %w", err)
	}
	return count, nil
}

func (r *UserRepository) UpdatePassword(ctx context.Context, id uuid.UUID, passwordHash string) error {
	result := r.db.WithContext(ctx).Model(&UserRow{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"password_hash": passwordHash,
			"updated_at":    time.Now().UTC(),
		})
	if result.Error != nil {
		return fmt.Errorf("更新密码失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrRepositoryNotFound
	}
	return nil
}

// UpdateEmail 更新用户 email。唯一约束冲突由 translateDBError 映射为 ErrConflict。
func (r *UserRepository) UpdateEmail(ctx context.Context, id uuid.UUID, email string) error {
	result := r.db.WithContext(ctx).Model(&UserRow{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"email":      nullableString(email),
			"updated_at": time.Now().UTC(),
		})
	if result.Error != nil {
		return translateDBError(result.Error, "更新 email 失败")
	}
	if result.RowsAffected == 0 {
		return ErrRepositoryNotFound
	}
	return nil
}

func (r *UserRepository) TouchLastLogin(ctx context.Context, id uuid.UUID) error {
	now := time.Now().UTC()
	result := r.db.WithContext(ctx).Model(&UserRow{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"last_login_at": now,
			"updated_at":    now,
		})
	if result.Error != nil {
		return fmt.Errorf("更新最后登录时间失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrRepositoryNotFound
	}
	return nil
}

// ResetPassword 在单个事务内原子地更新用户密码并撤销该用户的全部会话。
// 事务边界落在持有 *gorm.DB 的基础设施层；服务层仅调用此单一方法，
// 不感知数据库句柄。密码更新与会话删除要么全部成功，要么全部回滚。
//
// 该方法直接在同一 *gorm.DB 上删除 sessions 行（与 SessionRepository.DeleteAllForUser
// 操作的是同一张表），从而避免将 *gorm.DB 泄漏到服务层或跨仓储耦合。
func (r *UserRepository) ResetPassword(ctx context.Context, id uuid.UUID, passwordHash string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		result := tx.Model(&UserRow{}).
			Where("id = ?", id).
			Updates(map[string]any{
				"password_hash": passwordHash,
				"updated_at":    now,
			})
		if result.Error != nil {
			return translateDBError(result.Error, "重置密码失败")
		}
		if result.RowsAffected == 0 {
			return ErrRepositoryNotFound
		}
		// 撤销该用户的全部会话：物理删除以与 SessionRepository.DeleteAllForUser 语义一致。
		if err := tx.Where("user_id = ?", id).Delete(&SessionRow{}).Error; err != nil {
			return translateDBError(err, "重置密码删除会话失败")
		}
		return nil
	})
}

func userToRow(user *model.User) *UserRow {
	return &UserRow{
		ID:              user.ID,
		Email:           nullableString(user.Email),
		Nickname:        user.Nickname,
		PasswordHash:    user.PasswordHash,
		IsPlatformAdmin: user.IsPlatformAdmin,
		LastLoginAt:     user.LastLoginAt,
		CreatedAt:       user.CreatedAt,
		UpdatedAt:       user.UpdatedAt,
	}
}

func userFromRow(row *UserRow) *model.User {
	return &model.User{
		ID:              row.ID,
		Email:           dereferenceString(row.Email),
		Nickname:        row.Nickname,
		PasswordHash:    row.PasswordHash,
		IsPlatformAdmin: row.IsPlatformAdmin,
		LastLoginAt:     row.LastLoginAt,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}
