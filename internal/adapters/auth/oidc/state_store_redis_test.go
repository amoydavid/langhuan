package oidc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	authport "github.com/dajee/langhuan/internal/ports/auth"
)

func newMiniStateStore(t *testing.T) (*RedisStateStore, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("启动 miniredis 失败: %v", err)
	}
	t.Cleanup(mr.Close)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewRedisStateStore(client, 300), mr
}

func TestStateStoreIssueConsumeRoundTrip(t *testing.T) {
	store, _ := newMiniStateStore(t)
	ctx := context.Background()

	payload := authport.OIDCStatePayload{
		Next:         "/",
		BrowserNonce: "nonce-abc",
		OIDCNonce:    "oidc-nonce-xyz",
		PKCEVerifier: "verifier-123",
	}
	state, err := store.Issue(ctx, payload)
	if err != nil {
		t.Fatalf("Issue error: %v", err)
	}
	if state == "" {
		t.Fatal("state should be non-empty")
	}

	got, err := store.Consume(ctx, state, "nonce-abc")
	if err != nil {
		t.Fatalf("Consume error: %v", err)
	}
	if got.Next != "/" || got.BrowserNonce != "nonce-abc" || got.OIDCNonce != "oidc-nonce-xyz" || got.PKCEVerifier != "verifier-123" {
		t.Fatalf("payload mismatch: %+v", got)
	}
}

func TestStateStoreOneTimeConsume(t *testing.T) {
	store, _ := newMiniStateStore(t)
	ctx := context.Background()

	state, _ := store.Issue(ctx, authport.OIDCStatePayload{Next: "/", BrowserNonce: "n1"})
	if _, err := store.Consume(ctx, state, "n1"); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	// 第二次消费同 state 应失败（已删除）。
	if _, err := store.Consume(ctx, state, "n1"); err == nil {
		t.Fatal("second consume should fail (one-time)")
	}
}

func TestStateStoreRejectsMissingState(t *testing.T) {
	store, _ := newMiniStateStore(t)
	ctx := context.Background()
	if _, err := store.Consume(ctx, "nonexistent-state", "n1"); err == nil {
		t.Fatal("expected error for missing state")
	}
}

func TestStateStoreRejectsMismatchedNonce(t *testing.T) {
	store, mr := newMiniStateStore(t)
	ctx := context.Background()
	state, _ := store.Issue(ctx, authport.OIDCStatePayload{Next: "/", BrowserNonce: "correct-nonce"})
	// 错误 nonce 不应删除 state（防恶意消耗）。
	if _, err := store.Consume(ctx, state, "wrong-nonce"); err == nil {
		t.Fatal("expected error for mismatched nonce")
	}
	// state 应仍存在，可用正确 nonce 消费。
	if exists, _ := mr.Get("langhuan:oidc:state:" + state); exists == "" {
		t.Fatal("state should still exist after mismatched nonce")
	}
	if _, err := store.Consume(ctx, state, "correct-nonce"); err != nil {
		t.Fatalf("consume with correct nonce after failed attempt: %v", err)
	}
}

func TestStateStoreExpiresAfterTTL(t *testing.T) {
	store, mr := newMiniStateStore(t)
	ctx := context.Background()
	store.ttlSeconds = 1
	state, _ := store.Issue(ctx, authport.OIDCStatePayload{Next: "/", BrowserNonce: "n1"})

	mr.FastForward(2 * time.Second)

	if _, err := store.Consume(ctx, state, "n1"); err == nil {
		t.Fatal("expected error for expired state")
	}
}

func TestStateStoreConcurrentStatesIndependent(t *testing.T) {
	store, _ := newMiniStateStore(t)
	ctx := context.Background()
	// 两个并发 state，各自的 nonce 互不覆盖。
	s1, _ := store.Issue(ctx, authport.OIDCStatePayload{Next: "/a", BrowserNonce: "n1"})
	s2, _ := store.Issue(ctx, authport.OIDCStatePayload{Next: "/b", BrowserNonce: "n2"})

	p1, err := store.Consume(ctx, s1, "n1")
	if err != nil {
		t.Fatalf("consume s1: %v", err)
	}
	if p1.Next != "/a" {
		t.Fatalf("s1 next = %q", p1.Next)
	}
	p2, err := store.Consume(ctx, s2, "n2")
	if err != nil {
		t.Fatalf("consume s2: %v", err)
	}
	if p2.Next != "/b" {
		t.Fatalf("s2 next = %q", p2.Next)
	}
}

func TestStateStorePreservesAllPayloadFields(t *testing.T) {
	store, _ := newMiniStateStore(t)
	ctx := context.Background()
	actorID := uuid.New()
	sessionID := uuid.New()
	payload := authport.OIDCStatePayload{
		Next:                "/dashboard",
		BrowserNonce:        "bn",
		OIDCNonce:           "on",
		PKCEVerifier:        "pv",
		InvitationTokenHash: "abc123def",
		BindActorID:         actorID,
		BindSessionID:       sessionID,
	}
	state, _ := store.Issue(ctx, payload)
	got, err := store.Consume(ctx, state, "bn")
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if got.Next != "/dashboard" || got.BrowserNonce != "bn" || got.OIDCNonce != "on" ||
		got.PKCEVerifier != "pv" || got.InvitationTokenHash != "abc123def" ||
		got.BindActorID != actorID || got.BindSessionID != sessionID {
		t.Fatalf("payload fields not preserved: %+v", got)
	}
}

// 编译期断言：错误是非空（测试只验证失败发生，不绑死具体 error 类型）。
var _ = errors.New
