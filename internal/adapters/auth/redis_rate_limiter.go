package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	authport "github.com/dajee/langhuan/internal/ports/auth"
)

// 编译期断言：RedisRateLimiter 实现端口 auth.RateLimiter。
var _ authport.RateLimiter = (*RedisRateLimiter)(nil)

// rateLimitKeyPrefix 为登录失败计数的 Redis key 前缀。
const rateLimitKeyPrefix = "langhuan:login-failures:"

// RedisRateLimiter 基于 Redis 实现端口 auth.RateLimiter。
// 以 email 维度计数登录失败，超阈值则阻断。key 与计数均不含密码。
type RedisRateLimiter struct {
	client *redis.Client
}

// NewRedisRateLimiter 构造一个基于给定 redis.Client 的限流器。
func NewRedisRateLimiter(client *redis.Client) *RedisRateLimiter {
	return &RedisRateLimiter{client: client}
}

// IsBlocked 读取当前失败计数，count >= maxAttempts 即视为阻断。
// key 不存在（redis.Nil）时计数视为 0，不报错；其他 Redis 错误向上返回。
func (r *RedisRateLimiter) IsBlocked(ctx context.Context, email string, maxAttempts int) (bool, error) {
	key := rateLimitKey(email)
	count, err := r.client.Get(ctx, key).Int()
	if err != nil {
		if err == redis.Nil {
			return false, nil
		}
		return false, fmt.Errorf("限流: 读取失败计数失败: %w", err)
	}
	return count >= maxAttempts, nil
}

// RecordFailure 对计数器 INCR。仅在返回 count==1（首次失败）时设置 EXPIRE，
// 将窗口 TTL 锚定在首次失败。Redis 错误向上返回。
func (r *RedisRateLimiter) RecordFailure(ctx context.Context, email string, window time.Duration) error {
	key := rateLimitKey(email)
	count, err := r.client.Incr(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("限流: INCR 失败: %w", err)
	}
	if count == 1 {
		if err := r.client.Expire(ctx, key, window).Err(); err != nil {
			return fmt.Errorf("限流: 设置窗口 TTL 失败: %w", err)
		}
	}
	return nil
}

// Reset 删除计数器；删除不存在的 key 返回 0 计数而非错误。
func (r *RedisRateLimiter) Reset(ctx context.Context, email string) error {
	key := rateLimitKey(email)
	if err := r.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("限流: DEL 失败: %w", err)
	}
	return nil
}

// rateLimitKey 对规范化 email 计算 SHA-256，返回带前缀的 Redis key。
// 对 email 取摘要，使原始 email 不进入 Redis key。
func rateLimitKey(email string) string {
	normalized := strings.ToLower(strings.TrimSpace(email))
	sum := sha256.Sum256([]byte(normalized))
	digest := hex.EncodeToString(sum[:])
	return rateLimitKeyPrefix + digest
}
