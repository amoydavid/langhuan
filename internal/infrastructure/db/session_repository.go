package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/model"
)

// 编译期断言：基础设施层 SessionRepository 满足服务层定义的仓储接口。

var _ service.SessionRepository = (*SessionRepository)(nil)

type SessionRepository struct {
	db *gorm.DB
}

func NewSessionRepository(db *gorm.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

func (r *SessionRepository) Create(ctx context.Context, session *model.Session) error {
	if err := r.db.WithContext(ctx).Create(sessionToRow(session)).Error; err != nil {
		return translateDBError(err, "创建会话失败")
	}
	return nil
}

// FindActive 返回未撤销且未过期的会话；其余情形一律返回 ErrNotFound，
// 不向调用方区分“不存在/已过期/已撤销”以避免信息泄漏。
func (r *SessionRepository) FindActive(ctx context.Context, id uuid.UUID) (*model.Session, error) {
	var row SessionRow
	expiresCmp := "expires_at > now()"
	if r.db.Dialector.Name() == "sqlite" {
		expiresCmp = "expires_at > datetime('now')"
	}
	if err := r.db.WithContext(ctx).
		Where("id = ? AND revoked_at IS NULL AND "+expiresCmp, id).
		First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRepositoryNotFound
		}
		return nil, fmt.Errorf("读取会话失败: %w", err)
	}
	return sessionFromRow(&row), nil
}

func (r *SessionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	result := r.db.WithContext(ctx).Where("id = ?", id).Delete(&SessionRow{})
	if result.Error != nil {
		return fmt.Errorf("删除会话失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrRepositoryNotFound
	}
	return nil
}

func (r *SessionRepository) DeleteAllForUser(ctx context.Context, userID uuid.UUID) error {
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&SessionRow{}).Error; err != nil {
		return fmt.Errorf("删除用户会话失败: %w", err)
	}
	return nil
}

func sessionToRow(session *model.Session) *SessionRow {
	return &SessionRow{
		ID:         session.ID,
		UserID:     session.UserID,
		ExpiresAt:  session.ExpiresAt,
		CreatedAt:  session.CreatedAt,
		LastSeenAt: session.LastSeenAt,
		UserAgent:  session.UserAgent,
		IPAddr:     session.IPAddr,
		RevokedAt:  session.RevokedAt,
	}
}

func sessionFromRow(row *SessionRow) *model.Session {
	return &model.Session{
		ID:         row.ID,
		UserID:     row.UserID,
		ExpiresAt:  row.ExpiresAt,
		CreatedAt:  row.CreatedAt,
		LastSeenAt: row.LastSeenAt,
		UserAgent:  row.UserAgent,
		IPAddr:     row.IPAddr,
		RevokedAt:  row.RevokedAt,
	}
}
