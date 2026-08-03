package dto

import (
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
)

// AuthenticatedUser 是面向 API 的用户视图，仅暴露非敏感字段。
// 明文密码与密码哈希绝不进入该 DTO。会话 ID 由 AuthService 单独返回给 handler。
type AuthenticatedUser struct {
	ID              uuid.UUID `json:"id"`
	Email           string    `json:"email"`
	Nickname        string    `json:"nickname"`
	IsPlatformAdmin bool      `json:"is_platform_admin"`
}

// AuthenticatedUserFromModel 将领域用户转换为 AuthenticatedUser DTO。
func AuthenticatedUserFromModel(user *model.User) *AuthenticatedUser {
	if user == nil {
		return nil
	}
	return &AuthenticatedUser{
		ID:              user.ID,
		Email:           user.Email,
		Nickname:        user.Nickname,
		IsPlatformAdmin: user.IsPlatformAdmin,
	}
}
