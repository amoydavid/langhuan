package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// fakeKBValidator 是 MembershipKBValidator 的测试替身。
type fakeKBValidator struct {
	exist map[uuid.UUID]bool
	err   error
	calls int
}

func (f *fakeKBValidator) AllKnowledgeBasesExist(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (bool, error) {
	f.calls++
	if f.err != nil {
		return false, f.err
	}
	for _, id := range ids {
		if !f.exist[id] {
			return false, nil
		}
	}
	return true, nil
}

func newKBGrantsFixture(t *testing.T, targetRole value.WorkspaceRole, actorRole value.WorkspaceRole) (*MembershipService, *fakeMembershipRepository, *fakeKBValidator, uuid.UUID) {
	t.Helper()
	workspaceID := uuid.New()
	repo := newFakeMembershipRepository()
	memberID := uuid.New()
	actorID := uuid.New()
	mustCreateMembership(t, repo, workspaceID, memberID, targetRole)
	if actorID != memberID {
		mustCreateMembership(t, repo, workspaceID, actorID, actorRole)
	}
	validator := &fakeKBValidator{exist: map[uuid.UUID]bool{}}
	svc := NewMembershipService(repo, nil, validator)
	return svc, repo, validator, workspaceID
}

func mustCreateMembership(t *testing.T, repo *fakeMembershipRepository, workspaceID, userID uuid.UUID, role value.WorkspaceRole) {
	t.Helper()
	m, err := model.NewMembership(workspaceID, userID, role)
	if err != nil {
		t.Fatalf("NewMembership() error = %v", err)
	}
	if err := repo.Create(context.Background(), m); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
}

func TestMembershipServiceSetKBGrants(t *testing.T) {
	kbA := uuid.New()

	tests := []struct {
		name        string
		targetRole  value.WorkspaceRole
		actorRole   value.WorkspaceRole
		scope       value.KBScope
		kbIDs       []uuid.UUID
		exist       map[uuid.UUID]bool
		wantErr     error
		wantScope   value.KBScope // 期望持久化的 scope；nil 表示期望直接报错
		wantKBCount int
	}{
		{
			name:       "member 设为 all 成功",
			targetRole: value.RoleMember, actorRole: value.RoleAdmin,
			scope:     value.KBScopeAll,
			wantScope: value.KBScopeAll, wantKBCount: 0,
		},
		{
			name:       "member 设为 restricted 空集合成功（= 不可见任何库）",
			targetRole: value.RoleMember, actorRole: value.RoleOwner,
			scope:     value.KBScopeRestricted,
			wantScope: value.KBScopeRestricted, wantKBCount: 0,
		},
		{
			name:       "member 设为 restricted 带授权集合成功",
			targetRole: value.RoleMember, actorRole: value.RoleAdmin,
			scope: value.KBScopeRestricted, kbIDs: []uuid.UUID{kbA},
			exist:     map[uuid.UUID]bool{kbA: true},
			wantScope: value.KBScopeRestricted, wantKBCount: 1,
		},
		{
			name:       "授权集合含未知知识库拒绝",
			targetRole: value.RoleMember, actorRole: value.RoleAdmin,
			scope: value.KBScopeRestricted, kbIDs: []uuid.UUID{kbA},
			exist:   map[uuid.UUID]bool{},
			wantErr: domainerrors.ErrValidation,
		},
		{
			name:       "member 调用拒绝",
			targetRole: value.RoleMember, actorRole: value.RoleMember,
			scope:   value.KBScopeAll,
			wantErr: domainerrors.ErrForbidden,
		},
		{
			name:       "admin 目标拒绝（隐含全量，防困惑态）",
			targetRole: value.RoleAdmin, actorRole: value.RoleAdmin,
			scope:   value.KBScopeRestricted,
			wantErr: domainerrors.ErrValidation,
		},
		{
			name:       "owner 目标拒绝",
			targetRole: value.RoleOwner, actorRole: value.RoleAdmin,
			scope:   value.KBScopeAll,
			wantErr: domainerrors.ErrValidation,
		},
		{
			name:       "非法 scope 拒绝",
			targetRole: value.RoleMember, actorRole: value.RoleAdmin,
			scope:   value.KBScope("deny"),
			wantErr: domainerrors.ErrValidation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, validator, workspaceID := newKBGrantsFixture(t, tt.targetRole, tt.actorRole)
			validator.exist = tt.exist
			// 取目标成员 ID（构造时第一个成员是 target）。
			var targetID uuid.UUID
			for _, m := range repo.memberships {
				if m.Role == tt.targetRole {
					targetID = m.UserID
					break
				}
			}

			updated, err := svc.SetKBGrants(context.Background(), SetKBGrantsInput{
				WorkspaceID:      workspaceID,
				TargetUserID:     targetID,
				ActorRole:        tt.actorRole,
				Scope:            tt.scope,
				KnowledgeBaseIDs: tt.kbIDs,
			})
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("SetKBGrants() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("SetKBGrants() error = %v", err)
			}
			if updated.KBScope.Normalize() != tt.wantScope {
				t.Fatalf("KBScope = %q, want %q", updated.KBScope, tt.wantScope)
			}
			if len(repo.kbGrantsCalls) != 1 {
				t.Fatalf("UpdateKBGrants 调用次数 = %d, want 1", len(repo.kbGrantsCalls))
			}
			call := repo.kbGrantsCalls[0]
			if call.scope != tt.wantScope {
				t.Fatalf("持久化 scope = %q, want %q", call.scope, tt.wantScope)
			}
			if len(call.kbIDs) != tt.wantKBCount {
				t.Fatalf("持久化授权集合大小 = %d, want %d", len(call.kbIDs), tt.wantKBCount)
			}
		})
	}
}

func TestMembershipServiceSetKBGrantsMemberNotFound(t *testing.T) {
	svc, _, _, workspaceID := newKBGrantsFixture(t, value.RoleMember, value.RoleAdmin)

	_, err := svc.SetKBGrants(context.Background(), SetKBGrantsInput{
		WorkspaceID:  workspaceID,
		TargetUserID: uuid.New(),
		ActorRole:    value.RoleAdmin,
		Scope:        value.KBScopeAll,
	})
	if !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}
