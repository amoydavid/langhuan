package auth

import (
	"context"
	"time"
)

// RateLimiter 描述登录失败限流的端口抽象。
// 限流 key 与计数均不含密码；存储后端（如 Redis）的具体实现由适配器层提供。
type RateLimiter interface {
	// IsBlocked 判断指定 email 是否因失败次数达到阈值而被阻断。
	IsBlocked(ctx context.Context, email string, maxAttempts int) (bool, error)
	// RecordFailure 记录一次登录失败，window 为计数窗口的生存时间。
	// 首次失败时锚定窗口的 TTL。
	RecordFailure(ctx context.Context, email string, window time.Duration) error
	// Reset 清零指定 email 的失败计数。
	Reset(ctx context.Context, email string) error
}
