package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/model"
)

// 编译期断言：基础设施层 ExternalIdentityRepository 满足服务层定义的接口。
var _ service.ExternalIdentityReader = (*ExternalIdentityRepository)(nil)

// ExternalIdentityRepository 是 external_identities 的薄封装，只做 Row/领域转换与标准 CRUD。
type ExternalIdentityRepository struct {
	db *gorm.DB
}

// NewExternalIdentityRepository 构造 ExternalIdentityRepository。
func NewExternalIdentityRepository(db *gorm.DB) *ExternalIdentityRepository {
	return &ExternalIdentityRepository{db: db}
}

// ListByUserID 返回指定 user 的全部外部身份（事务外只读，供账号设置页）。
func (r *ExternalIdentityRepository) ListByUserID(ctx context.Context, userID uuid.UUID) ([]*model.ExternalIdentity, error) {
	var rows []ExternalIdentityRow
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("查询外部身份失败: %w", err)
	}
	result := make([]*model.ExternalIdentity, 0, len(rows))
	for i := range rows {
		result = append(result, externalIdentityFromRow(&rows[i]))
	}
	return result, nil
}

// externalIdentityToRow 把领域模型转换为 GORM Row。
func externalIdentityToRow(identity *model.ExternalIdentity) *ExternalIdentityRow {
	return &ExternalIdentityRow{
		ID:            identity.ID,
		UserID:        identity.UserID,
		Issuer:        identity.Issuer,
		Subject:       identity.Subject,
		Email:         identity.Email,
		EmailVerified: identity.EmailVerified,
		RawProfile:    identity.RawProfile,
		LastAuthAt:    identity.LastAuthAt,
		CreatedAt:     identity.CreatedAt,
		UpdatedAt:     identity.UpdatedAt,
	}
}

// externalIdentityFromRow 把 GORM Row 转换为领域模型。
func externalIdentityFromRow(row *ExternalIdentityRow) *model.ExternalIdentity {
	return &model.ExternalIdentity{
		ID:            row.ID,
		UserID:        row.UserID,
		Issuer:        row.Issuer,
		Subject:       row.Subject,
		Email:         row.Email,
		EmailVerified: row.EmailVerified,
		RawProfile:    row.RawProfile,
		LastAuthAt:    row.LastAuthAt,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}

// normalizeEmailForTx 复用领域层的 email 规范化逻辑（trim + lower）。
func normalizeEmailForTx(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
