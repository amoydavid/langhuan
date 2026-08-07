//go:build integration

package db

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestWorkspaceSlugGetBySlugIntegration(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	repo := NewWorkspaceRepository(tx)
	wsID := createWorkspaceRow(t, ctx, tx, "slug-test-ws")

	got, err := repo.GetBySlug(ctx, "slug-test-ws")
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if got.ID != wsID {
		t.Fatalf("id = %s, want %s", got.ID, wsID)
	}
	if got.Slug != "slug-test-ws" {
		t.Fatalf("slug = %q, want slug-test-ws", got.Slug)
	}

	if _, err := repo.GetBySlug(ctx, "does-not-exist"); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("missing slug err = %v, want ErrNotFound", err)
	}
}

func TestWorkspaceCreateWithOwnerIntegration(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	wsRepo := NewWorkspaceRepository(tx)
	userRepo := NewUserRepository(tx)
	mbRepo := NewMembershipRepository(tx)

	owner, err := model.NewUser("cw-owner@example.com", "Owner", "$argon2id$h")
	if err != nil {
		t.Fatal(err)
	}
	if err := userRepo.Create(ctx, owner); err != nil {
		t.Fatal(err)
	}

	ws, err := model.NewWorkspace("New WS", "new-ws-slug", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := wsRepo.CreateWithOwner(ctx, ws, owner.ID); err != nil {
		t.Fatalf("CreateWithOwner: %v", err)
	}

	got, err := wsRepo.GetBySlug(ctx, "new-ws-slug")
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if got.ID != ws.ID {
		t.Fatalf("workspace id = %s, want %s", got.ID, ws.ID)
	}

	mb, err := mbRepo.Get(ctx, ws.ID, owner.ID)
	if err != nil {
		t.Fatalf("owner membership not created: %v", err)
	}
	if mb.Role != value.RoleOwner {
		t.Fatalf("owner role = %q, want owner", mb.Role)
	}
}

// fakeRateLimiter is a no-op authport.RateLimiter used by TestV021AuthFlow so
// the end-to-end auth flow is never blocked by cross-test Redis rate-limit
// state. It records nothing and never blocks.
type fakeRateLimiter struct{}

func (fakeRateLimiter) IsBlocked(context.Context, string, int) (bool, error)       { return false, nil }
func (fakeRateLimiter) RecordFailure(context.Context, string, time.Duration) error { return nil }
func (fakeRateLimiter) Reset(context.Context, string) error                        { return nil }

func TestWorkspaceCreateWithOwnerIfEmptyRejectsSecondIntegration(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	wsRepo := NewWorkspaceRepository(tx)
	userRepo := NewUserRepository(tx)

	owner, err := model.NewUser("cwoe-owner@example.com", "Owner", "$argon2id$h")
	if err != nil {
		t.Fatal(err)
	}
	if err := userRepo.Create(ctx, owner); err != nil {
		t.Fatal(err)
	}

	first, err := model.NewWorkspace("First", "first-ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := wsRepo.CreateWithOwnerIfEmpty(ctx, first, owner.ID); err != nil {
		t.Fatalf("首次创建应成功: %v", err)
	}

	second, err := model.NewWorkspace("Second", "second-ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = wsRepo.CreateWithOwnerIfEmpty(ctx, second, owner.ID)
	if !errors.Is(err, domainerrors.ErrWorkspaceLimitReached) {
		t.Fatalf("第二次创建 err = %v, want ErrWorkspaceLimitReached", err)
	}
	// 第二次未落库：workspace 与其 owner membership 都应不存在。
	if _, err := wsRepo.GetBySlug(ctx, "second-ws"); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("second workspace should not exist, err = %v", err)
	}
}

func TestWorkspaceCreateWithOwnerIfEmptyConcurrentIntegration(t *testing.T) {
	// 空库并发创建：advisory lock 序列化后只有一个成功，其余得到 ErrWorkspaceLimitReached。
	ctx, gormDB := openIntegrationTestDB(t)
	wsRepo := NewWorkspaceRepository(gormDB)
	userRepo := NewUserRepository(gormDB)

	owner, err := model.NewUser("cwoe-conc@example.com", "Owner", "$argon2id$h")
	if err != nil {
		t.Fatal(err)
	}
	if err := userRepo.Create(ctx, owner); err != nil {
		t.Fatal(err)
	}

	const n = 4
	results := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			ws, err := model.NewWorkspace("Concurrent", "conc-ws-"+uuid.New().String(), nil)
			if err != nil {
				results[idx] = err
				return
			}
			results[idx] = wsRepo.CreateWithOwnerIfEmpty(ctx, ws, owner.ID)
		}(i)
	}
	wg.Wait()

	successCount := 0
	for _, err := range results {
		switch {
		case err == nil:
			successCount++
		case errors.Is(err, domainerrors.ErrWorkspaceLimitReached):
			// 预期中的并发拒绝
		default:
			t.Fatalf("并发创建出现意外错误: %v", err)
		}
	}
	if successCount != 1 {
		t.Fatalf("并发创建成功数 = %d, want 1", successCount)
	}

	var total int64
	if err := gormDB.Model(&WorkspaceRow{}).Count(&total).Error; err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("workspaces 总数 = %d, want 1", total)
	}
}
