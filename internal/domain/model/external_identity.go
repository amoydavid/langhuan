package model

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	id "github.com/dajee/langhuan/internal/domain/id"
)

// ExternalIdentity 记录用户与外部 OIDC issuer 的绑定。
// (issuer, subject) 全局唯一，指向唯一 user。一个 user 可绑定同一 issuer 下的
// 多个 subject，或不同 issuer 的身份；首版只配置一个内部 issuer。
type ExternalIdentity struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	Issuer        string // 运维配置的 OIDC issuer
	Subject       string // IdP 的 sub claim
	Email         string // 登录时刻快照，不允许为空
	EmailVerified bool
	RawProfile    string // 经过 whitelist 的 claims JSON
	LastAuthAt    time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// NewExternalIdentity 构造并校验：issuer/subject/email 非空，UserID 非 Nil。
// rawProfile 允许为空（调用方应传入经 whitelist 过滤的 JSON）。
func NewExternalIdentity(userID uuid.UUID, issuer, subject, email string, emailVerified bool, rawProfile string) (*ExternalIdentity, error) {
	if userID == uuid.Nil {
		return nil, fmt.Errorf("%w: user_id 不能为空", domainerrors.ErrValidation)
	}
	issuer = strings.TrimSpace(issuer)
	if issuer == "" {
		return nil, fmt.Errorf("%w: issuer 不能为空", domainerrors.ErrValidation)
	}
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return nil, fmt.Errorf("%w: subject 不能为空", domainerrors.ErrValidation)
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, fmt.Errorf("%w: email 不能为空", domainerrors.ErrValidation)
	}

	now := time.Now().UTC()
	return &ExternalIdentity{
		ID:            id.New(),
		UserID:        userID,
		Issuer:        issuer,
		Subject:       subject,
		Email:         email,
		EmailVerified: emailVerified,
		RawProfile:    rawProfile,
		LastAuthAt:    now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}
