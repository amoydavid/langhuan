package model

import (
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// User 表示平台用户。密码仅以 argon2id 编码字符串形式保存。
type User struct {
	ID              uuid.UUID
	Email           string
	Nickname        string
	PasswordHash    string
	IsPlatformAdmin bool
	LastLoginAt     *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// NewUser 创建并校验用户。email 会去除首尾空白并小写规范化；
// email 必须是纯净地址（拒绝带显示名形式），nickname 与 passwordHash 不能为空。
func NewUser(email, nickname, passwordHash string) (*User, error) {
	normalizedEmail, err := normalizeEmail(email)
	if err != nil {
		return nil, err
	}

	nickname = strings.TrimSpace(nickname)
	if nickname == "" {
		return nil, fmt.Errorf("%w: nickname 不能为空", domainerrors.ErrValidation)
	}

	passwordHash = strings.TrimSpace(passwordHash)
	if passwordHash == "" {
		return nil, fmt.Errorf("%w: password_hash 不能为空", domainerrors.ErrValidation)
	}

	now := time.Now().UTC()
	return &User{
		ID:           uuid.New(),
		Email:        normalizedEmail,
		Nickname:     nickname,
		PasswordHash: passwordHash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

// normalizeEmail 将 email 去除首尾空白并小写规范化，同时要求它是一个纯净的邮箱地址
// （拒绝带显示名形式，即 mail.ParseAddress 解析后的地址必须等于规范化后的输入）。
func normalizeEmail(email string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))
	if normalized == "" {
		return "", fmt.Errorf("%w: email 不能为空", domainerrors.ErrValidation)
	}
	parsed, err := mail.ParseAddress(normalized)
	if err != nil {
		return "", fmt.Errorf("%w: email 格式无效", domainerrors.ErrValidation)
	}
	if parsed.Address != normalized {
		return "", fmt.Errorf("%w: email 必须为纯净地址", domainerrors.ErrValidation)
	}
	return normalized, nil
}
