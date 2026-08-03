package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// newMiniRateLimiter 启动一个 miniredis 并构造基于它的限流器，返回用于断言的辅助对象。
func newMiniRateLimiter(t *testing.T) (*RedisRateLimiter, *miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("启动 miniredis 失败: %v", err)
	}
	t.Cleanup(mr.Close)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewRedisRateLimiter(client), mr, client
}

func TestRedisRateLimiterBlocksAfterMaxAttempts(t *testing.T) {
	limiter, _, _ := newMiniRateLimiter(t)
	ctx := context.Background()
	const email = "user@example.com"
	const maxAttempts = 5
	window := 15 * time.Minute

	// 记录 4 次失败：尚未达到阈值，不应阻断。
	for i := 0; i < 4; i++ {
		if err := limiter.RecordFailure(ctx, email, window); err != nil {
			t.Fatalf("RecordFailure #%d 返回错误: %v", i+1, err)
		}
	}
	if blocked, err := limiter.IsBlocked(ctx, email, maxAttempts); err != nil {
		t.Fatalf("IsBlocked 返回错误: %v", err)
	} else if blocked {
		t.Fatal("4 次失败后不应阻断")
	}

	// 第 5 次失败达到阈值：之后应被阻断。
	if err := limiter.RecordFailure(ctx, email, window); err != nil {
		t.Fatalf("第 5 次 RecordFailure 返回错误: %v", err)
	}
	if blocked, err := limiter.IsBlocked(ctx, email, maxAttempts); err != nil {
		t.Fatalf("IsBlocked 返回错误: %v", err)
	} else if !blocked {
		t.Fatal("5 次失败后应阻断")
	}
}

func TestRedisRateLimiterResetClearsCounter(t *testing.T) {
	limiter, _, _ := newMiniRateLimiter(t)
	ctx := context.Background()
	const email = "reset@example.com"
	const maxAttempts = 5

	// 达到阻断状态。
	for i := 0; i < maxAttempts; i++ {
		if err := limiter.RecordFailure(ctx, email, 15*time.Minute); err != nil {
			t.Fatalf("RecordFailure #%d 返回错误: %v", i+1, err)
		}
	}
	if blocked, _ := limiter.IsBlocked(ctx, email, maxAttempts); !blocked {
		t.Fatal("应先达到阻断状态")
	}

	// Reset 后计数清零，不再阻断。
	if err := limiter.Reset(ctx, email); err != nil {
		t.Fatalf("Reset 返回错误: %v", err)
	}
	if blocked, err := limiter.IsBlocked(ctx, email, maxAttempts); err != nil {
		t.Fatalf("Reset 后 IsBlocked 返回错误: %v", err)
	} else if blocked {
		t.Fatal("Reset 后不应阻断")
	}
}

func TestRedisRateLimiterFastForwardExpiresWindow(t *testing.T) {
	limiter, mr, _ := newMiniRateLimiter(t)
	ctx := context.Background()
	const email = "expire@example.com"
	const maxAttempts = 5
	window := 15 * time.Minute

	for i := 0; i < maxAttempts; i++ {
		if err := limiter.RecordFailure(ctx, email, window); err != nil {
			t.Fatalf("RecordFailure #%d 返回错误: %v", i+1, err)
		}
	}
	if blocked, _ := limiter.IsBlocked(ctx, email, maxAttempts); !blocked {
		t.Fatal("应先达到阻断状态")
	}

	// 推进 miniredis 虚拟时钟超过窗口 TTL，key 过期。
	mr.FastForward(window + time.Second)

	if blocked, err := limiter.IsBlocked(ctx, email, maxAttempts); err != nil {
		t.Fatalf("窗口过期后 IsBlocked 返回错误: %v", err)
	} else if blocked {
		t.Fatal("窗口过期后不应阻断")
	}
}

// TestRedisRateLimiterKeyIsEmailDigest 验证 Redis key 是规范化 email 的 SHA-256，
// 且前缀为 langhuan:login-failures:。不暴露原始 email。
func TestRedisRateLimiterKeyIsEmailDigest(t *testing.T) {
	limiter, mr, _ := newMiniRateLimiter(t)
	ctx := context.Background()
	const rawEmail = "  User@Example.COM  "

	if err := limiter.RecordFailure(ctx, rawEmail, 15*time.Minute); err != nil {
		t.Fatalf("RecordFailure 返回错误: %v", err)
	}

	// 规范化：TrimSpace + ToLower。
	normalized := strings.ToLower(strings.TrimSpace(rawEmail))
	sum := sha256.Sum256([]byte(normalized))
	expectedDigest := hex.EncodeToString(sum[:])
	expectedKey := fmt.Sprintf("langhuan:login-failures:%s", expectedDigest)

	keys := mr.Keys()
	found := false
	for _, k := range keys {
		if k == expectedKey {
			found = true
			break
		}
		if strings.Contains(k, "@") {
			t.Fatalf("Redis key 不应包含原始 email: %q", k)
		}
	}
	if !found {
		t.Fatalf("未找到预期 key %q, 现有 keys: %v", expectedKey, keys)
	}
	if !strings.HasPrefix(expectedKey, "langhuan:login-failures:") {
		t.Fatalf("key 前缀异常: %q", expectedKey)
	}
}
