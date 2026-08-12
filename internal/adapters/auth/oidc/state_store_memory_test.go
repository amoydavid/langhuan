package oidc

import (
	"context"
	"testing"
	"time"

	authport "github.com/dajee/langhuan/internal/ports/auth"
)

func TestMemoryStateStoreIssueConsume(t *testing.T) {
	s := NewMemoryStateStore(time.Minute)
	defer s.Close()
	ctx := context.Background()
	payload := authport.OIDCStatePayload{
		Next:         "/dashboard",
		BrowserNonce: "browser-nonce-xyz",
		OIDCNonce:    "oidc-nonce",
	}
	state, err := s.Issue(ctx, payload)
	if err != nil {
		t.Fatalf("Issue 失败: %v", err)
	}
	if state == "" {
		t.Fatal("state 不应为空")
	}
	got, err := s.Consume(ctx, state, "browser-nonce-xyz")
	if err != nil {
		t.Fatalf("Consume 失败: %v", err)
	}
	if got.Next != "/dashboard" {
		t.Fatalf("payload.Next = %q", got.Next)
	}
}

func TestMemoryStateStoreOneTimeConsumption(t *testing.T) {
	s := NewMemoryStateStore(time.Minute)
	defer s.Close()
	ctx := context.Background()
	state, _ := s.Issue(ctx, authport.OIDCStatePayload{BrowserNonce: "n"})
	if _, err := s.Consume(ctx, state, "n"); err != nil {
		t.Fatal(err)
	}
	// 二次消费应失败
	if _, err := s.Consume(ctx, state, "n"); err == nil {
		t.Fatal("state 一次性消费，二次应失败")
	}
}

func TestMemoryStateStoreNonceMismatchReissues(t *testing.T) {
	s := NewMemoryStateStore(time.Minute)
	defer s.Close()
	ctx := context.Background()
	state, _ := s.Issue(ctx, authport.OIDCStatePayload{BrowserNonce: "correct"})
	// nonce 不匹配：应失败但保留 state
	if _, err := s.Consume(ctx, state, "wrong"); err == nil {
		t.Fatal("nonce 不匹配应失败")
	}
	// 正确 nonce 仍可消费（state 被回写）
	if _, err := s.Consume(ctx, state, "correct"); err != nil {
		t.Fatalf("nonce 不匹配后正确 nonce 应可消费: %v", err)
	}
}

func TestMemoryStateStoreInvalidState(t *testing.T) {
	s := NewMemoryStateStore(time.Minute)
	defer s.Close()
	if _, err := s.Consume(context.Background(), "nonexistent", "n"); err == nil {
		t.Fatal("不存在的 state 应失败")
	}
	// 超长 state 应失败
	longState := make([]byte, 200)
	for i := range longState {
		longState[i] = 'a'
	}
	if _, err := s.Consume(context.Background(), string(longState), "n"); err == nil {
		t.Fatal("超长 state 应失败")
	}
}
