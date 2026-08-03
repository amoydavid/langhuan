package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// ---------------------------------------------------------------------------
// Fakes for MembershipRepository
// ---------------------------------------------------------------------------

// membershipKey 唯一标识一条 membership。
type membershipKey struct {
	workspace uuid.UUID
	user      uuid.UUID
}

type fakeMembershipRepository struct {
	mu          sync.Mutex
	memberships map[membershipKey]*model.Membership
	ownerCounts map[uuid.UUID]int64 // 显式覆盖的 owner 计数（默认按数据实时计算）
	changeErr   error
	deleteErr   error
}

func newFakeMembershipRepository() *fakeMembershipRepository {
	return &fakeMembershipRepository{
		memberships: make(map[membershipKey]*model.Membership),
		ownerCounts: make(map[uuid.UUID]int64),
	}
}

func (r *fakeMembershipRepository) Create(_ context.Context, m *model.Membership) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.memberships[membershipKey{m.WorkspaceID, m.UserID}] = m
	return nil
}

func (r *fakeMembershipRepository) Get(_ context.Context, workspaceID, userID uuid.UUID) (*model.Membership, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := r.memberships[membershipKey{workspaceID, userID}]; ok {
		return m, nil
	}
	return nil, domainerrors.ErrNotFound
}

func (r *fakeMembershipRepository) List(_ context.Context, workspaceID uuid.UUID) ([]*model.Membership, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []*model.Membership
	for k, m := range r.memberships {
		if k.workspace == workspaceID {
			result = append(result, m)
		}
	}
	return result, nil
}

func (r *fakeMembershipRepository) ListByUserID(_ context.Context, userID uuid.UUID) ([]*model.Membership, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []*model.Membership
	for k, m := range r.memberships {
		if k.user == userID {
			result = append(result, m)
		}
	}
	return result, nil
}

func (r *fakeMembershipRepository) ChangeRole(_ context.Context, workspaceID, userID uuid.UUID, role value.WorkspaceRole) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.changeErr != nil {
		return r.changeErr
	}
	m, ok := r.memberships[membershipKey{workspaceID, userID}]
	if !ok {
		return domainerrors.ErrNotFound
	}
	m.Role = role
	m.UpdatedAt = time.Now().UTC()
	return nil
}

func (r *fakeMembershipRepository) Delete(_ context.Context, workspaceID, userID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.deleteErr != nil {
		return r.deleteErr
	}
	delete(r.memberships, membershipKey{workspaceID, userID})
	return nil
}

// CountOwners 实时统计 workspace 内 owner 数量。
func (r *fakeMembershipRepository) CountOwners(_ context.Context, workspaceID uuid.UUID) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if count, ok := r.ownerCounts[workspaceID]; ok {
		return count, nil
	}
	var count int64
	for k, m := range r.memberships {
		if k.workspace == workspaceID && m.Role == value.RoleOwner {
			count++
		}
	}
	return count, nil
}

// seedMembership 写入一条 membership 并返回它。
func seedMembership(repo *fakeMembershipRepository, workspaceID, userID uuid.UUID, role value.WorkspaceRole) *model.Membership {
	m, err := model.NewMembership(workspaceID, userID, role)
	if err != nil {
		panic(err)
	}
	_ = repo.Create(context.Background(), m)
	return m
}

// ---------------------------------------------------------------------------
// MembershipService.List
// ---------------------------------------------------------------------------

func TestMembershipList(t *testing.T) {
	repo := newFakeMembershipRepository()
	svc := NewMembershipService(repo, newFakeUserRepository())
	ws := uuid.New()

	seedMembership(repo, ws, uuid.New(), value.RoleOwner)
	seedMembership(repo, ws, uuid.New(), value.RoleMember)

	got, err := svc.List(context.Background(), ws)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 memberships, got %d", len(got))
	}
}

func TestMembershipListIncludesUsersWithSingleBatchLookup(t *testing.T) {
	repo := newFakeMembershipRepository()
	users := newFakeUserRepository()
	workspaceID := uuid.New()
	first, err := model.NewUser("first@example.com", "张三", "secret-hash")
	if err != nil {
		t.Fatal(err)
	}
	second, err := model.NewUser("second@example.com", "李四", "secret-hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := users.Create(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := users.Create(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	seedMembership(repo, workspaceID, first.ID, value.RoleOwner)
	seedMembership(repo, workspaceID, second.ID, value.RoleMember)

	got, err := NewMembershipService(repo, users).List(context.Background(), workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if users.listByIDsCalls != 1 {
		t.Fatalf("ListByIDs called %d times, want 1", users.listByIDsCalls)
	}
	queriedIDs := make(map[uuid.UUID]bool, len(users.listByIDsIDs))
	for _, id := range users.listByIDsIDs {
		queriedIDs[id] = true
	}
	if len(queriedIDs) != 2 || !queriedIDs[first.ID] || !queriedIDs[second.ID] {
		t.Fatalf("ListByIDs ids = %v", users.listByIDsIDs)
	}
	if len(got) != 2 {
		t.Fatalf("len(List()) = %d", len(got))
	}
	byID := make(map[uuid.UUID]*dto.Membership, len(got))
	for _, membership := range got {
		byID[membership.UserID] = membership
	}
	if byID[first.ID].User.Email != first.Email || byID[first.ID].User.Nickname != first.Nickname {
		t.Fatalf("user summary = %#v", byID[first.ID].User)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "password_hash") || strings.Contains(string(raw), "secret-hash") || strings.Contains(string(raw), "last_login") {
		t.Fatalf("membership response leaked sensitive user fields: %s", raw)
	}
}

func TestMembershipListForUserDoesNotEnrichUsers(t *testing.T) {
	repo := newFakeMembershipRepository()
	users := newFakeUserRepository()
	userID := uuid.New()
	seedMembership(repo, uuid.New(), userID, value.RoleMember)

	got, err := NewMembershipService(repo, users).ListForUser(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len(ListForUser()) = %d", len(got))
	}
	if users.listByIDsCalls != 0 {
		t.Fatalf("ListForUser unexpectedly queried users %d times", users.listByIDsCalls)
	}
}

// ---------------------------------------------------------------------------
// MembershipService.ChangeRole
// ---------------------------------------------------------------------------

func TestMembershipChangeRoleRequiresOwner(t *testing.T) {
	repo := newFakeMembershipRepository()
	svc := NewMembershipService(repo, newFakeUserRepository())
	ws := uuid.New()
	target := uuid.New()
	seedMembership(repo, ws, target, value.RoleMember)

	cases := []value.WorkspaceRole{value.RoleMember, value.RoleAdmin}
	for _, actor := range cases {
		_, err := svc.ChangeRole(context.Background(), ws, target, value.RoleAdmin, actor)
		if !errors.Is(err, domainerrors.ErrForbidden) {
			t.Fatalf("actor %s: err = %v, want ErrForbidden", actor, err)
		}
	}
}

func TestMembershipChangeRoleOwnerCanPromoteSecondOwner(t *testing.T) {
	repo := newFakeMembershipRepository()
	svc := NewMembershipService(repo, newFakeUserRepository())
	ws := uuid.New()
	existingOwner := uuid.New()
	target := uuid.New()
	seedMembership(repo, ws, existingOwner, value.RoleOwner)
	seedMembership(repo, ws, target, value.RoleMember)

	got, err := svc.ChangeRole(context.Background(), ws, target, value.RoleOwner, value.RoleOwner)
	if err != nil {
		t.Fatalf("ChangeRole returned error: %v", err)
	}
	if got.Role != value.RoleOwner {
		t.Fatalf("role = %v, want owner", got.Role)
	}
	// 提升后存在两个 owner。
	count, err := repo.CountOwners(context.Background(), ws)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("owner count = %d, want 2", count)
	}
}

func TestMembershipChangeRoleRejectsDowngradeLastOwner(t *testing.T) {
	repo := newFakeMembershipRepository()
	svc := NewMembershipService(repo, newFakeUserRepository())
	ws := uuid.New()
	soleOwner := uuid.New()
	seedMembership(repo, ws, soleOwner, value.RoleOwner)

	_, err := svc.ChangeRole(context.Background(), ws, soleOwner, value.RoleAdmin, value.RoleOwner)
	if !errors.Is(err, domainerrors.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict (cannot downgrade last owner)", err)
	}
	// 角色不应被修改。
	m, _ := repo.Get(context.Background(), ws, soleOwner)
	if m.Role != value.RoleOwner {
		t.Fatalf("role = %v, want unchanged owner", m.Role)
	}
}

func TestMembershipChangeRoleAllowsAfterOwnershipTransfer(t *testing.T) {
	repo := newFakeMembershipRepository()
	svc := NewMembershipService(repo, newFakeUserRepository())
	ws := uuid.New()
	firstOwner := uuid.New()
	secondOwner := uuid.New()
	seedMembership(repo, ws, firstOwner, value.RoleOwner)
	seedMembership(repo, ws, secondOwner, value.RoleMember)

	// 先把第二名提升为 owner（转移/共享所有权）。
	if _, err := svc.ChangeRole(context.Background(), ws, secondOwner, value.RoleOwner, value.RoleOwner); err != nil {
		t.Fatalf("promote second owner: %v", err)
	}
	// 现在可以降级首位 owner（已不再是唯一 owner）。
	if _, err := svc.ChangeRole(context.Background(), ws, firstOwner, value.RoleAdmin, value.RoleOwner); err != nil {
		t.Fatalf("demote first owner after transfer: %v", err)
	}
	m, _ := repo.Get(context.Background(), ws, firstOwner)
	if m.Role != value.RoleAdmin {
		t.Fatalf("role = %v, want admin", m.Role)
	}
}

// ---------------------------------------------------------------------------
// MembershipService.Remove
// ---------------------------------------------------------------------------

func TestMembershipRemoveRequiresOwner(t *testing.T) {
	repo := newFakeMembershipRepository()
	svc := NewMembershipService(repo, newFakeUserRepository())
	ws := uuid.New()
	target := uuid.New()
	seedMembership(repo, ws, target, value.RoleMember)

	err := svc.Remove(context.Background(), ws, target, value.RoleAdmin)
	if !errors.Is(err, domainerrors.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if _, ok := repo.memberships[membershipKey{ws, target}]; !ok {
		t.Fatal("membership must not be deleted when actor is not owner")
	}
}

func TestMembershipRemoveRejectsLastOwner(t *testing.T) {
	repo := newFakeMembershipRepository()
	svc := NewMembershipService(repo, newFakeUserRepository())
	ws := uuid.New()
	soleOwner := uuid.New()
	seedMembership(repo, ws, soleOwner, value.RoleOwner)

	err := svc.Remove(context.Background(), ws, soleOwner, value.RoleOwner)
	if !errors.Is(err, domainerrors.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if _, ok := repo.memberships[membershipKey{ws, soleOwner}]; !ok {
		t.Fatal("last owner must not be deleted")
	}
}

func TestMembershipRemoveMember(t *testing.T) {
	repo := newFakeMembershipRepository()
	svc := NewMembershipService(repo, newFakeUserRepository())
	ws := uuid.New()
	member := uuid.New()
	owner := uuid.New()
	seedMembership(repo, ws, owner, value.RoleOwner)
	seedMembership(repo, ws, member, value.RoleMember)

	if err := svc.Remove(context.Background(), ws, member, value.RoleOwner); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}
	if _, ok := repo.memberships[membershipKey{ws, member}]; ok {
		t.Fatal("member should be deleted")
	}
}

func TestMembershipGet(t *testing.T) {
	repo := newFakeMembershipRepository()
	svc := NewMembershipService(repo, newFakeUserRepository())
	ws := uuid.New()
	user := uuid.New()
	seedMembership(repo, ws, user, value.RoleMember)

	got, err := svc.Get(context.Background(), ws, user)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.UserID != user {
		t.Fatalf("user id = %s, want %s", got.UserID, user)
	}
}
