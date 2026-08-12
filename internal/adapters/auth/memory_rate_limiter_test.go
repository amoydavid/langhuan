package auth

import (
	"context"
	"testing"
	"time"
)

func TestMemoryRateLimiterBlocksAfterThreshold(t *testing.T) {
	rl := NewMemoryRateLimiter()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := rl.RecordFailure(ctx, "user@example.com", time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	blocked, err := rl.IsBlocked(ctx, "user@example.com", 3)
	if err != nil {
		t.Fatal(err)
	}
	if !blocked {
		t.Fatal("失败 3 次后应阻断（阈值 3）")
	}
	// 另一 email 不受影响
	blocked2, _ := rl.IsBlocked(ctx, "other@example.com", 3)
	if blocked2 {
		t.Fatal("另一 email 不应被阻断")
	}
}

func TestMemoryRateLimiterResetClears(t *testing.T) {
	rl := NewMemoryRateLimiter()
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_ = rl.RecordFailure(ctx, "x@x.com", time.Minute)
	}
	if err := rl.Reset(ctx, "x@x.com"); err != nil {
		t.Fatal(err)
	}
	blocked, _ := rl.IsBlocked(ctx, "x@x.com", 3)
	if blocked {
		t.Fatal("Reset 后不应阻断")
	}
}

func TestMemoryRateLimiterWindowExpiry(t *testing.T) {
	rl := NewMemoryRateLimiter()
	ctx := context.Background()
	// 极短窗口，模拟过期
	for i := 0; i < 3; i++ {
		_ = rl.RecordFailure(ctx, "y@y.com", 20*time.Millisecond)
	}
	blocked, _ := rl.IsBlocked(ctx, "y@y.com", 3)
	if !blocked {
		t.Fatal("窗口内应阻断")
	}
	time.Sleep(30 * time.Millisecond)
	// 过期后再记录一次，计数应重新开始
	_ = rl.RecordFailure(ctx, "y@y.com", 20*time.Millisecond)
	blocked2, _ := rl.IsBlocked(ctx, "y@y.com", 3)
	if blocked2 {
		t.Fatal("窗口过期后重置，单次失败不应阻断")
	}
}

func TestMemoryRateLimiterKeyIsEmailDigest(t *testing.T) {
	// 大小写/空格归一化后应命中同一 key
	rl := NewMemoryRateLimiter()
	ctx := context.Background()
	_ = rl.RecordFailure(ctx, "User@Example.com", time.Minute)
	blocked, _ := rl.IsBlocked(ctx, "  user@example.com  ", 1)
	if !blocked {
		t.Fatal("email 归一化后应命中同一计数")
	}
}
