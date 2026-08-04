package model

import (
	"fmt"
	id "github.com/dajee/langhuan/internal/domain/id"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// Session 表示一个已认证会话。会话 ID 不得进入日志或响应。
type Session struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	ExpiresAt  time.Time
	CreatedAt  time.Time
	LastSeenAt time.Time
	UserAgent  string
	IPAddr     string
	RevokedAt  *time.Time
}

// NewSession 创建会话，ID 使用 id.New() 生成（与其它模型一致），过期时间为 now + lifetime。
func NewSession(userID uuid.UUID, lifetime time.Duration, userAgent, ipAddr string) (*Session, error) {
	if userID == uuid.Nil {
		return nil, fmt.Errorf("%w: user_id 不能为空", domainerrors.ErrValidation)
	}
	if lifetime <= 0 {
		return nil, fmt.Errorf("%w: lifetime 必须大于 0", domainerrors.ErrValidation)
	}

	now := time.Now().UTC()
	return &Session{
		ID:         id.New(),
		UserID:     userID,
		ExpiresAt:  now.Add(lifetime),
		CreatedAt:  now,
		LastSeenAt: now,
		UserAgent:  userAgent,
		IPAddr:     ipAddr,
	}, nil
}
