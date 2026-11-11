//go:build integration

package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	appservice "github.com/dajee/langhuan/internal/application/service"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// createKBRowForGrant 插入一条最小知识库行（满足绑定表复合 FK），返回 ID。
func createKBRowForGrant(t *testing.T, ctx context.Context, tx *gorm.DB, workspaceID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	kb, err := model.NewKnowledgeBase(workspaceID, name, "", uuid.New(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	row := &KnowledgeBaseRow{
		ID: kb.ID, WorkspaceID: workspaceID, Name: name,
		Metadata:       JSONMap{},
		FileTreeRootID: uuid.New(),
		SourceType:     string(value.SourceTypeUpload),
		SourceConfig:   JSONMap{},
		CreatedAt:      now, UpdatedAt: now,
	}
	if err := tx.WithContext(ctx).Create(row).Error; err != nil {
		t.Fatalf("create kb row: %v", err)
	}
	return kb.ID
}

func TestMembershipKBGrantsIntegration(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	userRepo := NewUserRepository(tx)
	mbRepo := NewMembershipRepository(tx)
	workspaceID := createWorkspaceRow(t, ctx, tx, "kb-grants-ws")

	user, err := model.NewUser("grants@example.com", "GR", "$argon2id$h")
	if err != nil {
		t.Fatal(err)
	}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	mb, err := model.NewMembership(workspaceID, user.ID, value.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if err := mbRepo.Create(ctx, mb); err != nil {
		t.Fatal(err)
	}

	kbA := createKBRowForGrant(t, ctx, tx, workspaceID, "grants-a")
	kbB := createKBRowForGrant(t, ctx, tx, workspaceID, "grants-b")
	otherWS := createWorkspaceRow(t, ctx, tx, "kb-grants-other")
	kbForeign := createKBRowForGrant(t, ctx, tx, otherWS, "grants-foreign")

	kbRepo := NewKnowledgeBaseRepository(tx)

	// 初始：存量行为 all，Get 不带授权集合。
	got, err := mbRepo.Get(ctx, workspaceID, user.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.KBScope != value.KBScopeAll {
		t.Fatalf("初始 kb_scope = %q, want all", got.KBScope)
	}

	// 授权校验：跨 workspace / 未知 ID 视为不存在。
	exist, err := kbRepo.AllKnowledgeBasesExist(ctx, workspaceID, []uuid.UUID{kbA, kbForeign})
	if err != nil {
		t.Fatalf("AllKnowledgeBasesExist: %v", err)
	}
	if exist {
		t.Fatal("跨 workspace 知识库应视为不存在")
	}
	exist, err = kbRepo.AllKnowledgeBasesExist(ctx, workspaceID, []uuid.UUID{kbA, kbB})
	if err != nil || !exist {
		t.Fatalf("本 workspace 集合应全部存在: exist=%v err=%v", exist, err)
	}

	// 设为 restricted + 授权集合。
	updated, err := mbRepo.UpdateKBGrants(ctx, workspaceID, user.ID, value.KBScopeRestricted, []uuid.UUID{kbA, kbB, kbA})
	if err != nil {
		t.Fatalf("UpdateKBGrants: %v", err)
	}
	if updated.KBScope != value.KBScopeRestricted || len(updated.KnowledgeBaseIDs) != 2 {
		t.Fatalf("scope/ids = %q %d, want restricted/2（重复 ID 去重）", updated.KBScope, len(updated.KnowledgeBaseIDs))
	}

	// Get/List 均加载授权集合。
	got, err = mbRepo.Get(ctx, workspaceID, user.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.KnowledgeBaseIDs) != 2 {
		t.Fatalf("Get grants = %d, want 2", len(got.KnowledgeBaseIDs))
	}
	list, err := mbRepo.List(ctx, workspaceID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var listed *model.Membership
	for _, m := range list {
		if m.UserID == user.ID {
			listed = m
		}
	}
	if listed == nil || len(listed.KnowledgeBaseIDs) != 2 {
		t.Fatalf("List grants 加载失败: %#v", listed)
	}

	// 整体替换（幂等）：收缩到单库。
	if _, err := mbRepo.UpdateKBGrants(ctx, workspaceID, user.ID, value.KBScopeRestricted, []uuid.UUID{kbA}); err != nil {
		t.Fatalf("UpdateKBGrants replace: %v", err)
	}
	got, _ = mbRepo.Get(ctx, workspaceID, user.ID)
	if len(got.KnowledgeBaseIDs) != 1 || got.KnowledgeBaseIDs[0] != kbA {
		t.Fatalf("替换后 grants = %v, want [kbA]", got.KnowledgeBaseIDs)
	}

	// 切回 all：清空绑定行。
	if _, err := mbRepo.UpdateKBGrants(ctx, workspaceID, user.ID, value.KBScopeAll, []uuid.UUID{kbA}); err != nil {
		t.Fatalf("UpdateKBGrants all: %v", err)
	}
	got, _ = mbRepo.Get(ctx, workspaceID, user.ID)
	if len(got.KnowledgeBaseIDs) != 0 {
		t.Fatalf("all 后 grants = %v, want 空", got.KnowledgeBaseIDs)
	}

	// D4 端到端：受限 member 通过 service.Create 建库，新库自动进入授权集合。
	if _, err := mbRepo.UpdateKBGrants(ctx, workspaceID, user.ID, value.KBScopeRestricted, []uuid.UUID{kbA}); err != nil {
		t.Fatal(err)
	}
	providerRepo := NewModelProviderRepository(tx)
	modelRepo := NewModelRepository(tx)
	provider := createProviderForTest(t, ctx, providerRepo, value.ModelScopeWorkspace, &workspaceID, "grants-provider")
	embeddingModel := createModelForTest(t, ctx, modelRepo, provider.ID, "grants-embedding", value.ModelStatusActive)
	kbService := appservice.NewKnowledgeBaseService(kbRepo, kbRepo)
	created, err := kbService.Create(ctx, appservice.CreateKnowledgeBaseInput{
		WorkspaceID: workspaceID, Name: "grants-d4", EmbeddingModelID: embeddingModel.ID,
		ActorUserID: &user.ID,
		Access:      value.ResourceAccess{WorkspaceID: workspaceID, AllowedKnowledgeBaseIDs: []uuid.UUID{kbA}},
	})
	if err != nil {
		t.Fatalf("受限建库: %v", err)
	}
	got, _ = mbRepo.Get(ctx, workspaceID, user.ID)
	found := false
	for _, id := range got.KnowledgeBaseIDs {
		if id == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("新建知识库未进入创建者授权集合: %v", got.KnowledgeBaseIDs)
	}

	// all 成员建库不写授权行（隐含全量，无需显式授权）。
	user2, err := model.NewUser("grants-all@example.com", "GA", "$argon2id$h")
	if err != nil {
		t.Fatal(err)
	}
	if err := userRepo.Create(ctx, user2); err != nil {
		t.Fatal(err)
	}
	mb2, err := model.NewMembership(workspaceID, user2.ID, value.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if err := mbRepo.Create(ctx, mb2); err != nil {
		t.Fatal(err)
	}
	if _, err := kbService.Create(ctx, appservice.CreateKnowledgeBaseInput{
		WorkspaceID: workspaceID, Name: "grants-all-created", EmbeddingModelID: embeddingModel.ID,
		ActorUserID: &user2.ID,
		Access:      value.ResourceAccess{WorkspaceID: workspaceID, Unrestricted: true},
	}); err != nil {
		t.Fatalf("all 成员建库: %v", err)
	}
	got2, _ := mbRepo.Get(ctx, workspaceID, user2.ID)
	if got2.KBScope != value.KBScopeAll {
		t.Fatalf("all 成员 kb_scope = %q, want all", got2.KBScope)
	}

	// 目标成员不存在 → ErrRepositoryNotFound。
	if _, err := mbRepo.UpdateKBGrants(ctx, workspaceID, uuid.New(), value.KBScopeAll, nil); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("missing member err = %v, want ErrRepositoryNotFound", err)
	}

	// 非法 scope → ErrValidation。
	if _, err := mbRepo.UpdateKBGrants(ctx, workspaceID, user.ID, value.KBScope("deny"), nil); !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("invalid scope err = %v, want ErrValidation", err)
	}
}
