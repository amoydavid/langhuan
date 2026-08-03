package value

import "time"

// APIKeyStatus 描述 Workspace API Key 的派生状态。
//
// 状态不单独存列，由 revoked_at/expires_at 与当前时间派生；expiring 只是
// UI 提醒，鉴权上仍为 active。
type APIKeyStatus string

const (
	// APIKeyStatusActive 表示 key 处于可用状态。
	APIKeyStatusActive APIKeyStatus = "active"
	// APIKeyStatusExpiring 表示 key 将在 7 天内到期，鉴权仍为 active。
	APIKeyStatusExpiring APIKeyStatus = "expiring"
	// APIKeyStatusExpired 表示 key 已到期，鉴权失败。
	APIKeyStatusExpired APIKeyStatus = "expired"
	// APIKeyStatusRevoked 表示 key 已被吊销，鉴权失败。
	APIKeyStatusRevoked APIKeyStatus = "revoked"
)

const apiKeyExpiringWindow = 7 * 24 * time.Hour

// DeriveAPIKeyStatus 按 revoked_at -> expires_at(NULL=不限期) -> 即将到期
// 窗口 -> active 的固定顺序派生状态。now 通常来自注入的 Clock。
func DeriveAPIKeyStatus(revokedAt *time.Time, expiresAt *time.Time, now time.Time) APIKeyStatus {
	if revokedAt != nil {
		return APIKeyStatusRevoked
	}
	if expiresAt == nil {
		return APIKeyStatusActive
	}
	if !expiresAt.After(now) {
		return APIKeyStatusExpired
	}
	if expiresAt.Sub(now) <= apiKeyExpiringWindow {
		return APIKeyStatusExpiring
	}
	return APIKeyStatusActive
}
