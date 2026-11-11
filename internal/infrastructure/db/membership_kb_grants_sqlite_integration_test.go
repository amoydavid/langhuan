//go:build integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// TestMembershipKBGrantsSQLiteDialect 覆盖 000006 迁移后的成员授权基线：
// kb_scope 读写、绑定表复合主键与查询（standalone 模式与 PG 行为等价）。
func TestMembershipKBGrantsSQLiteDialect(t *testing.T) {
	database := openSQLiteTestDB(t)
	ctx := context.Background()

	userRepo := NewUserRepository(database)
	mbRepo := NewMembershipRepository(database)

	wsID := uuid.New()
	now := time.Now().UTC()
	if err := database.WithContext(ctx).Create(&WorkspaceRow{
		ID: wsID, Name: "sqlite-grants", Slug: "sqlite-grants",
		Metadata: JSONMap{}, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	user, err := model.NewUser("sqlite-grants@example.com", "SG", "$argon2id$h")
	if err != nil {
		t.Fatal(err)
	}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	mb, err := model.NewMembership(wsID, user.ID, value.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if err := mbRepo.Create(ctx, mb); err != nil {
		t.Fatalf("create membership: %v", err)
	}

	kbA, kbB := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{kbA, kbB} {
		row := &KnowledgeBaseRow{
			ID: id, WorkspaceID: wsID, Name: "sqlite-kb-" + id.String()[:8],
			Metadata: JSONMap{}, FileTreeRootID: uuid.New(),
			SourceType: string(value.SourceTypeUpload), SourceConfig: JSONMap{},
			CreatedAt: now, UpdatedAt: now,
		}
		if err := database.WithContext(ctx).Create(row).Error; err != nil {
			t.Fatalf("create kb row: %v", err)
		}
	}

	updated, err := mbRepo.UpdateKBGrants(ctx, wsID, user.ID, value.KBScopeRestricted, []uuid.UUID{kbA, kbB})
	if err != nil {
		t.Fatalf("UpdateKBGrants: %v", err)
	}
	if updated.KBScope != value.KBScopeRestricted || len(updated.KnowledgeBaseIDs) != 2 {
		t.Fatalf("scope/ids = %q/%d, want restricted/2", updated.KBScope, len(updated.KnowledgeBaseIDs))
	}

	got, err := mbRepo.Get(ctx, wsID, user.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.KnowledgeBaseIDs) != 2 {
		t.Fatalf("grants = %v, want 2 项", got.KnowledgeBaseIDs)
	}

	if _, err := mbRepo.UpdateKBGrants(ctx, wsID, user.ID, value.KBScopeAll, nil); err != nil {
		t.Fatalf("回切 all: %v", err)
	}
	got, _ = mbRepo.Get(ctx, wsID, user.ID)
	if len(got.KnowledgeBaseIDs) != 0 || got.KBScope != value.KBScopeAll {
		t.Fatalf("回切后 scope/ids = %q/%v, want all/空", got.KBScope, got.KnowledgeBaseIDs)
	}
}
