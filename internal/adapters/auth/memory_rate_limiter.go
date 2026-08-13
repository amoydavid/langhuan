package auth

import (
	"context"
	"sync"
	"time"

	authport "github.com/dajee/langhuan/internal/ports/auth"
)

// 编译期断言：MemoryRateLimiter 实现端口 auth.RateLimiter。
var _ authport.RateLimiter = (*MemoryRateLimiter)(nil)

// MemoryRateLimiter 是 RateLimiter 的进程内内存实现，供 standalone 无 Redis 模式使用。
//
// 语义与 RedisRateLimiter 对齐：以规范化 email 的 SHA-256 为 key 计数登录失败，
// 超阈值阻断；窗口 TTL 锚定在首次失败。单进程下比 Redis 更准（无网络错误路径）。
// 重启清零（standalone 定位可接受）；key 不含明文密码。
type MemoryRateLimiter struct {
	mu        sync.Mutex
	counts    map[string]int
	firstSeen map[string]time.Time
}

// NewMemoryRateLimiter 构造一个内存限流器。
func NewMemoryRateLimiter() *MemoryRateLimiter {
	return &MemoryRateLimiter{
		counts:    make(map[string]int),
		firstSeen: make(map[string]time.Time),
	}
}

// IsBlocked 返回当前失败计数是否已达阈值。窗口过期时计数视为 0。
func (m *MemoryRateLimiter) IsBlocked(ctx context.Context, email string, maxAttempts int) (bool, error) {
	key := rateLimitKey(email)
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counts[key] >= maxAttempts, nil
}

// RecordFailure 对计数器 +1。首次失败锚定窗口起点；窗口超时则重置计数并重新锚定。
func (m *MemoryRateLimiter) RecordFailure(ctx context.Context, email string, window time.Duration) error {
	key := rateLimitKey(email)
	m.mu.Lock()
	defer m.mu.Unlock()
	start, ok := m.firstSeen[key]
	if ok && time.Since(start) >= window {
		// 上一窗口已过，重新开始
		m.counts[key] = 0
		delete(m.firstSeen, key)
	}
	if _, exists := m.firstSeen[key]; !exists {
		m.firstSeen[key] = time.Now()
	}
	m.counts[key]++
	return nil
}

// Reset 清零指定 email 的失败计数与窗口。
func (m *MemoryRateLimiter) Reset(ctx context.Context, email string) error {
	key := rateLimitKey(email)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.counts, key)
	delete(m.firstSeen, key)
	return nil
}
