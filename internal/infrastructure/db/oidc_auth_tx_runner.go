package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
)

// 编译期断言：OIDCAuthTxRunner 满足服务层定义的事务 runner 接口。
var _ service.OIDCAuthTxRunner = (*OIDCAuthTxRunner)(nil)

// bootstrapLockKey 是 advisory lock 的稳定 key（hashtextextended 的文本种子）。
const bootstrapLockKey = "langhuan:auth-bootstrap"

// OIDCAuthTxRunner 实现 service.OIDCAuthTxRunner，在 db.Transaction 内提供
// tx-bound 薄持久化操作。业务分支留在 service，本类型只建事务与持久化。
type OIDCAuthTxRunner struct {
	db *gorm.DB
}

// NewOIDCAuthTxRunner 构造 OIDCAuthTxRunner。
func NewOIDCAuthTxRunner(db *gorm.DB) *OIDCAuthTxRunner {
	return &OIDCAuthTxRunner{db: db}
}

// WithinOIDCAuth 开启事务并把 tx-bound 薄持久化接口传入回调。
func (r *OIDCAuthTxRunner) WithinOIDCAuth(ctx context.Context, fn func(tx service.OIDCAuthTx) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&oidcAuthTx{tx: tx})
	})
}

// oidcAuthTx 是事务内的薄持久化操作实现，包裹 *gorm.DB tx。
type oidcAuthTx struct {
	tx *gorm.DB
}

// AcquireBootstrapLock 获取 bootstrap advisory transaction lock。
// 所有建 user 路径在 CountUsers 前必须调用，保证首管理员判定原子。
func (t *oidcAuthTx) AcquireBootstrapLock(ctx context.Context) error {
	return t.tx.WithContext(ctx).Exec(
		"SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", bootstrapLockKey,
	).Error
}

func (t *oidcAuthTx) CountUsers(ctx context.Context) (int64, error) {
	var count int64
	if err := t.tx.WithContext(ctx).Model(&UserRow{}).Count(&count).Error; err != nil {
		return 0, fmt.Errorf("统计用户失败: %w", err)
	}
	return count, nil
}

func (t *oidcAuthTx) FindIdentityByIssuerSubject(ctx context.Context, issuer, subject string) (*model.ExternalIdentity, error) {
	var row ExternalIdentityRow
	err := t.tx.WithContext(ctx).Where("issuer = ? AND subject = ?", issuer, subject).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainerrors.ErrNotFound
		}
		return nil, fmt.Errorf("查找外部身份失败: %w", err)
	}
	return externalIdentityFromRow(&row), nil
}

func (t *oidcAuthTx) FindUserByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	var row UserRow
	err := t.tx.WithContext(ctx).First(&row, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainerrors.ErrNotFound
		}
		return nil, fmt.Errorf("查找用户失败: %w", err)
	}
	return userFromRow(&row), nil
}

func (t *oidcAuthTx) FindUserByEmail(ctx context.Context, email string) (*model.User, error) {
	var row UserRow
	err := t.tx.WithContext(ctx).First(&row, "email = ?", normalizeEmailForTx(email)).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainerrors.ErrNotFound
		}
		return nil, fmt.Errorf("查找用户失败: %w", err)
	}
	return userFromRow(&row), nil
}

func (t *oidcAuthTx) CreateUser(ctx context.Context, user *model.User) error {
	if err := t.tx.WithContext(ctx).Create(userToRow(user)).Error; err != nil {
		return translateDBError(err, "创建用户失败")
	}
	return nil
}

func (t *oidcAuthTx) CreateIdentity(ctx context.Context, identity *model.ExternalIdentity) error {
	if err := t.tx.WithContext(ctx).Create(externalIdentityToRow(identity)).Error; err != nil {
		return translateDBError(err, "创建外部身份失败")
	}
	return nil
}

func (t *oidcAuthTx) UpdateIdentityAuth(ctx context.Context, identity *model.ExternalIdentity, rawProfile string) error {
	now := time.Now().UTC()
	result := t.tx.WithContext(ctx).Model(&ExternalIdentityRow{}).
		Where("id = ?", identity.ID).
		Updates(map[string]any{
			"raw_profile":    rawProfile,
			"last_auth_at":   now,
			"email":          identity.Email,
			"email_verified": identity.EmailVerified,
			"updated_at":     now,
		})
	if result.Error != nil {
		return fmt.Errorf("更新外部身份失败: %w", result.Error)
	}
	return nil
}

func (t *oidcAuthTx) CreateSession(ctx context.Context, session *model.Session) error {
	if err := t.tx.WithContext(ctx).Create(sessionToRow(session)).Error; err != nil {
		return translateDBError(err, "创建会话失败")
	}
	return nil
}

func (t *oidcAuthTx) TouchLastLogin(ctx context.Context, userID uuid.UUID) error {
	now := time.Now().UTC()
	return t.tx.WithContext(ctx).Model(&UserRow{}).Where("id = ?", userID).
		Update("last_login_at", now).Error
}

func (t *oidcAuthTx) FindActiveSession(ctx context.Context, sessionID uuid.UUID) (*model.Session, error) {
	var row SessionRow
	err := t.tx.WithContext(ctx).
		Where("id = ? AND revoked_at IS NULL AND expires_at > ?", sessionID, time.Now().UTC()).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainerrors.ErrNotFound
		}
		return nil, fmt.Errorf("查找会话失败: %w", err)
	}
	return sessionFromRow(&row), nil
}

func (t *oidcAuthTx) FindPendingInvitationForUpdate(ctx context.Context, tokenHash string) (*model.Invitation, error) {
	var row InvitationRow
	err := t.tx.WithContext(ctx).
		Where("token_hash = ? AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > ?", tokenHash, time.Now().UTC()).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domainerrors.ErrNotFound
		}
		return nil, fmt.Errorf("查找邀请失败: %w", err)
	}
	return invitationFromRow(&row), nil
}

func (t *oidcAuthTx) CreateMembership(ctx context.Context, membership *model.Membership) error {
	if err := t.tx.WithContext(ctx).Create(membershipToRow(membership)).Error; err != nil {
		return translateDBError(err, "创建成员关系失败")
	}
	return nil
}

func (t *oidcAuthTx) MarkInvitationAccepted(ctx context.Context, invitationID, userID uuid.UUID) error {
	now := time.Now().UTC()
	result := t.tx.WithContext(ctx).Model(&InvitationRow{}).
		Where("id = ? AND accepted_at IS NULL AND revoked_at IS NULL", invitationID).
		Updates(map[string]any{
			"accepted_at":      now,
			"accepted_user_id": userID,
		})
	if result.Error != nil {
		return translateDBError(result.Error, "标记邀请已接受失败")
	}
	if result.RowsAffected == 0 {
		return domainerrors.ErrConflict
	}
	return nil
}
