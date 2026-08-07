package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/value"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
)

type fakeWorkspaceRepository struct {
	items     map[uuid.UUID]*model.Workspace
	bySlug    map[string]*model.Workspace
	defaultID uuid.UUID
	// createWithOwnerCalls 记录 CreateWithOwner 的调用与 owner membership，
	// 用于断言“workspace 与 owner membership 同一事务”。
	createWithOwnerCalls int
	ownerMemberships     map[uuid.UUID]value.WorkspaceRole // workspaceID -> role 记录事务内创建的 owner 角色
	createErr            error                             // 可注入 CreateWithOwner/Create 的错误
	// createWithOwnerIfEmptyCalls 记录 CreateWithOwnerIfEmpty 的调用次数，
	// 用于断言单租户模式走原子条件创建分支。
	createWithOwnerIfEmptyCalls int
}

func newFakeWorkspaceRepository() *fakeWorkspaceRepository {
	return &fakeWorkspaceRepository{
		items:            make(map[uuid.UUID]*model.Workspace),
		bySlug:           make(map[string]*model.Workspace),
		ownerMemberships: make(map[uuid.UUID]value.WorkspaceRole),
	}
}

func (r *fakeWorkspaceRepository) Create(_ context.Context, ws *model.Workspace) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.items[ws.ID] = ws
	if r.bySlug != nil {
		if _, exists := r.bySlug[ws.Slug]; exists {
			return domainerrors.ErrConflict
		}
		r.bySlug[ws.Slug] = ws
	}
	if r.defaultID == uuid.Nil {
		r.defaultID = ws.ID
	}
	return nil
}

func (r *fakeWorkspaceRepository) Get(_ context.Context, id uuid.UUID) (*model.Workspace, error) {
	ws, ok := r.items[id]
	if !ok {
		return nil, domainerrors.ErrNotFound
	}
	return ws, nil
}

func (r *fakeWorkspaceRepository) GetDefault(ctx context.Context) (*model.Workspace, error) {
	if r.defaultID == uuid.Nil {
		return nil, domainerrors.ErrNotFound
	}
	return r.Get(ctx, r.defaultID)
}

// GetBySlug 按 slug 查询 workspace。
func (r *fakeWorkspaceRepository) GetBySlug(_ context.Context, slug string) (*model.Workspace, error) {
	ws, ok := r.bySlug[slug]
	if !ok {
		return nil, domainerrors.ErrNotFound
	}
	return ws, nil
}

// CreateWithOwner 模拟“创建 workspace + owner membership 同一事务”：
// 写入 workspace 行并记录一条 owner 成员关系。
func (r *fakeWorkspaceRepository) CreateWithOwner(_ context.Context, ws *model.Workspace, ownerUserID uuid.UUID) error {
	if r.createErr != nil {
		return r.createErr
	}
	if _, exists := r.bySlug[ws.Slug]; exists {
		return domainerrors.ErrConflict
	}
	r.items[ws.ID] = ws
	r.bySlug[ws.Slug] = ws
	r.createWithOwnerCalls++
	r.ownerMemberships[ws.ID] = value.RoleOwner
	return nil
}

// CreateWithOwnerIfEmpty 模拟单租户原子条件创建：仅当尚无 workspace 时创建，
// 否则返回 ErrWorkspaceLimitReached（真实实现对应用户层的 advisory lock + 计数）。
func (r *fakeWorkspaceRepository) CreateWithOwnerIfEmpty(ctx context.Context, ws *model.Workspace, ownerUserID uuid.UUID) error {
	r.createWithOwnerIfEmptyCalls++
	if len(r.items) > 0 {
		return domainerrors.ErrWorkspaceLimitReached
	}
	return r.CreateWithOwner(ctx, ws, ownerUserID)
}

func TestWorkspaceServiceCreateAndGet(t *testing.T) {
	svc := NewWorkspaceService(newFakeWorkspaceRepository(), false)

	created, err := svc.Create(context.Background(), CreateWorkspaceInput{
		Name: "Acme",
		Slug: "acme",
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID {
		t.Fatalf("id = %s, want %s", got.ID, created.ID)
	}
	if got.Name != "Acme" {
		t.Fatalf("name = %q", got.Name)
	}
	if got.Slug != "acme" {
		t.Fatalf("slug = %q, want %q", got.Slug, "acme")
	}
}

func TestWorkspaceServiceCreateValidatesInput(t *testing.T) {
	svc := NewWorkspaceService(newFakeWorkspaceRepository(), false)

	if _, err := svc.Create(context.Background(), CreateWorkspaceInput{Name: "", Slug: "acme"}); err == nil {
		t.Fatal("expected name validation error")
	}
}

func TestWorkspaceServiceGetNotFound(t *testing.T) {
	svc := NewWorkspaceService(newFakeWorkspaceRepository(), false)

	_, err := svc.Get(context.Background(), uuid.New())
	if !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestWorkspaceServiceGetDefault(t *testing.T) {
	svc := NewWorkspaceService(newFakeWorkspaceRepository(), false)

	created, err := svc.Create(context.Background(), CreateWorkspaceInput{Name: "Default", Slug: "default"})
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.GetDefault(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID {
		t.Fatalf("id = %s, want %s", got.ID, created.ID)
	}
}

// ---------------------------------------------------------------------------
// WorkspaceService.CreateForPlatformAdmin / GetBySlug
// ---------------------------------------------------------------------------

func TestWorkspaceCreateForPlatformAdminRequiresPlatformAdmin(t *testing.T) {
	repo := newFakeWorkspaceRepository()
	svc := NewWorkspaceService(repo, false)
	creator := uuid.New()

	_, err := svc.CreateForPlatformAdmin(context.Background(), CreateWorkspaceInput{
		Name: "Acme", Slug: "acme",
	}, creator, false)
	if !errors.Is(err, domainerrors.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	// 非管理员不得创建 workspace，事务不应执行。
	if repo.createWithOwnerCalls != 0 {
		t.Fatalf("CreateWithOwner must not run for non-admin; calls=%d", repo.createWithOwnerCalls)
	}
}

func TestWorkspaceCreateForPlatformAdminCreatesWorkspaceAndOwnerMembership(t *testing.T) {
	repo := newFakeWorkspaceRepository()
	svc := NewWorkspaceService(repo, false)
	creator := uuid.New()

	got, err := svc.CreateForPlatformAdmin(context.Background(), CreateWorkspaceInput{
		Name: "Acme", Slug: "acme",
	}, creator, true)
	if err != nil {
		t.Fatalf("CreateForPlatformAdmin returned error: %v", err)
	}
	// 返回的 DTO 带 slug。
	if got.Slug != "acme" {
		t.Fatalf("slug = %q, want acme", got.Slug)
	}
	// 单一事务：CreateWithOwner 被调用一次，并记录了 owner 成员关系。
	if repo.createWithOwnerCalls != 1 {
		t.Fatalf("CreateWithOwner calls = %d, want 1", repo.createWithOwnerCalls)
	}
	if repo.ownerMemberships[got.ID] != value.RoleOwner {
		t.Fatalf("expected owner membership recorded for workspace %s, got %v", got.ID, repo.ownerMemberships[got.ID])
	}
}

func TestWorkspaceCreateForPlatformAdminValidatesSlug(t *testing.T) {
	repo := newFakeWorkspaceRepository()
	svc := NewWorkspaceService(repo, false)

	_, err := svc.CreateForPlatformAdmin(context.Background(), CreateWorkspaceInput{
		Name: "Acme", Slug: "BAD SLUG!",
	}, uuid.New(), true)
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if repo.createWithOwnerCalls != 0 {
		t.Fatalf("CreateWithOwner must not run on validation failure; calls=%d", repo.createWithOwnerCalls)
	}
}

func TestWorkspaceCreateForPlatformAdminConflictOnDuplicateSlug(t *testing.T) {
	repo := newFakeWorkspaceRepository()
	repo.createErr = domainerrors.ErrConflict // 模拟 slug 唯一冲突
	svc := NewWorkspaceService(repo, false)

	_, err := svc.CreateForPlatformAdmin(context.Background(), CreateWorkspaceInput{
		Name: "Acme", Slug: "acme",
	}, uuid.New(), true)
	if !errors.Is(err, domainerrors.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestWorkspaceGetBySlug(t *testing.T) {
	repo := newFakeWorkspaceRepository()
	svc := NewWorkspaceService(repo, false)

	ws, err := model.NewWorkspace("Acme", "acme", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = repo.Create(context.Background(), ws)

	got, err := svc.GetBySlug(context.Background(), "acme")
	if err != nil {
		t.Fatalf("GetBySlug returned error: %v", err)
	}
	if got.ID != ws.ID {
		t.Fatalf("id = %s, want %s", got.ID, ws.ID)
	}

	if _, err := svc.GetBySlug(context.Background(), "missing"); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// ---------------------------------------------------------------------------
// 单租户模式：workspace 数量限制
// ---------------------------------------------------------------------------

func TestWorkspaceCreateForPlatformAdminSingleTenantAllowsFirst(t *testing.T) {
	repo := newFakeWorkspaceRepository()
	svc := NewWorkspaceService(repo, true) // singleTenant=true（OIDC 开启）
	creator := uuid.New()

	got, err := svc.CreateForPlatformAdmin(context.Background(), CreateWorkspaceInput{
		Name: "Acme", Slug: "acme",
	}, creator, true)
	if err != nil {
		t.Fatalf("首次创建应成功: %v", err)
	}
	// 单租户必须走原子条件创建分支。
	if repo.createWithOwnerIfEmptyCalls != 1 {
		t.Fatalf("CreateWithOwnerIfEmpty calls = %d, want 1", repo.createWithOwnerIfEmptyCalls)
	}
	if repo.ownerMemberships[got.ID] != value.RoleOwner {
		t.Fatalf("expected owner membership for %s, got %v", got.ID, repo.ownerMemberships[got.ID])
	}
}

func TestWorkspaceCreateForPlatformAdminSingleTenantRejectsSecond(t *testing.T) {
	repo := newFakeWorkspaceRepository()
	svc := NewWorkspaceService(repo, true) // singleTenant=true（OIDC 开启）
	creator := uuid.New()

	if _, err := svc.CreateForPlatformAdmin(context.Background(), CreateWorkspaceInput{
		Name: "Acme", Slug: "acme",
	}, creator, true); err != nil {
		t.Fatalf("首次创建应成功: %v", err)
	}

	_, err := svc.CreateForPlatformAdmin(context.Background(), CreateWorkspaceInput{
		Name: "Beta", Slug: "beta",
	}, creator, true)
	if !errors.Is(err, domainerrors.ErrWorkspaceLimitReached) {
		t.Fatalf("第二次创建 err = %v, want ErrWorkspaceLimitReached", err)
	}
	// 两次都走了原子条件创建分支，第二次由仓储拒绝。
	if repo.createWithOwnerIfEmptyCalls != 2 {
		t.Fatalf("CreateWithOwnerIfEmpty calls = %d, want 2", repo.createWithOwnerIfEmptyCalls)
	}
}

func TestWorkspaceCreateForPlatformAdminMultiTenantAllowsMultiple(t *testing.T) {
	repo := newFakeWorkspaceRepository()
	svc := NewWorkspaceService(repo, false) // 多租户（密码模式）不受数量限制
	creator := uuid.New()

	if _, err := svc.CreateForPlatformAdmin(context.Background(), CreateWorkspaceInput{
		Name: "Acme", Slug: "acme",
	}, creator, true); err != nil {
		t.Fatalf("第一次创建应成功: %v", err)
	}
	second, err := svc.CreateForPlatformAdmin(context.Background(), CreateWorkspaceInput{
		Name: "Beta", Slug: "beta",
	}, creator, true)
	if err != nil {
		t.Fatalf("多租户第二次创建应成功: %v", err)
	}
	if second.Slug != "beta" {
		t.Fatalf("slug = %q, want beta", second.Slug)
	}
	// 多租户不调用原子条件创建分支。
	if repo.createWithOwnerIfEmptyCalls != 0 {
		t.Fatalf("CreateWithOwnerIfEmpty calls = %d, want 0", repo.createWithOwnerIfEmptyCalls)
	}
}
