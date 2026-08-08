package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// newFeishuKBForPolicyTest 在 fake repo 上创建一个飞书来源知识库，并返回它。
func newFeishuKBForPolicyTest(t *testing.T, repository *fakeKnowledgeBaseRepository, workspaceID uuid.UUID, sourceConfig map[string]any) uuid.UUID {
	t.Helper()
	resolved := fakeResolvedEmbeddingModel(t, value.ModelScopePlatform, nil, value.ModelStatusActive, value.ModelStatusActive)
	repository.models[resolved.Model.ID] = resolved
	connID := uuid.New()
	service := NewKnowledgeBaseService(repository, repository).WithSyncEnqueuer(&fakeKBSyncEnqueuer{}, nil)
	created, err := service.Create(context.Background(), CreateKnowledgeBaseInput{
		WorkspaceID: workspaceID, Name: "飞书库", EmbeddingModelID: resolved.Model.ID,
		SourceType: value.SourceTypeFeishuWiki, SourceConfig: sourceConfig, SourceConnectionID: &connID,
	})
	if err != nil {
		t.Fatalf("create feishu kb: %v", err)
	}
	return created.ID
}

// TestCreateFeishuKnowledgeBaseDefaultsOnDeleteToKeep 验证创建飞书 KB 时缺失 on_delete 补默认 keep。
func TestCreateFeishuKnowledgeBaseDefaultsOnDeleteToKeep(t *testing.T) {
	workspaceID := uuid.New()
	repository := newFakeKnowledgeBaseRepository()
	kbID := newFeishuKBForPolicyTest(t, repository, workspaceID, map[string]any{"root_token": "wikcn-root"})

	kb := repository.items[kbID]
	if got := kb.SourceConfig["on_delete"]; got != value.SourceDeleteKeep.String() {
		t.Fatalf("on_delete = %v, want %q", got, value.SourceDeleteKeep.String())
	}
}

// TestCreateFeishuKnowledgeBasePersistsExplicitOnDelete 验证显式合法 on_delete 被归一化后落库。
func TestCreateFeishuKnowledgeBasePersistsExplicitOnDelete(t *testing.T) {
	workspaceID := uuid.New()
	repository := newFakeKnowledgeBaseRepository()
	kbID := newFeishuKBForPolicyTest(t, repository, workspaceID, map[string]any{
		"root_token": "wikcn-root", "on_delete": "REMOVE",
	})

	kb := repository.items[kbID]
	if got := kb.SourceConfig["on_delete"]; got != value.SourceDeleteRemove.String() {
		t.Fatalf("on_delete = %v, want %q", got, value.SourceDeleteRemove.String())
	}
}

// TestCreateFeishuKnowledgeBaseRejectsInvalidOnDelete 验证创建时非法 on_delete 返回 ErrValidation。
func TestCreateFeishuKnowledgeBaseRejectsInvalidOnDelete(t *testing.T) {
	workspaceID := uuid.New()
	repository := newFakeKnowledgeBaseRepository()
	resolved := fakeResolvedEmbeddingModel(t, value.ModelScopePlatform, nil, value.ModelStatusActive, value.ModelStatusActive)
	repository.models[resolved.Model.ID] = resolved
	connID := uuid.New()
	service := NewKnowledgeBaseService(repository, repository)

	_, err := service.Create(context.Background(), CreateKnowledgeBaseInput{
		WorkspaceID: workspaceID, Name: "飞书库", EmbeddingModelID: resolved.Model.ID,
		SourceType: value.SourceTypeFeishuWiki,
		SourceConfig: map[string]any{
			"root_token": "wikcn-root", "on_delete": "purge",
		},
		SourceConnectionID: &connID,
	})
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if repository.sourcePolicyCalls != 0 {
		t.Fatalf("policy updater should not be called at create; calls = %d", repository.sourcePolicyCalls)
	}
}

// TestCreateFeishuKnowledgeBaseRejectsNonStringOnDelete 验证非字符串 on_delete 返回 ErrValidation。
func TestCreateFeishuKnowledgeBaseRejectsNonStringOnDelete(t *testing.T) {
	workspaceID := uuid.New()
	repository := newFakeKnowledgeBaseRepository()
	resolved := fakeResolvedEmbeddingModel(t, value.ModelScopePlatform, nil, value.ModelStatusActive, value.ModelStatusActive)
	repository.models[resolved.Model.ID] = resolved
	connID := uuid.New()
	service := NewKnowledgeBaseService(repository, repository)

	_, err := service.Create(context.Background(), CreateKnowledgeBaseInput{
		WorkspaceID: workspaceID, Name: "飞书库", EmbeddingModelID: resolved.Model.ID,
		SourceType: value.SourceTypeFeishuWiki,
		SourceConfig: map[string]any{
			"root_token": "wikcn-root", "on_delete": 123,
		},
		SourceConnectionID: &connID,
	})
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

// TestUpdateSourceDeletePolicyUpdatesFeishuKBOtherKeysPreserved 验证策略更新只改 on_delete，
// 其余运行期键保留，且策略被归一化写入。
func TestUpdateSourceDeletePolicyUpdatesFeishuKBOtherKeysPreserved(t *testing.T) {
	workspaceID := uuid.New()
	repository := newFakeKnowledgeBaseRepository()
	kbID := newFeishuKBForPolicyTest(t, repository, workspaceID, map[string]any{
		"root_token": "wikcn-root", "sync_cursor": "2026-08-07T00:00:00Z",
		"cron": "0 * * * *", "on_delete": "keep",
	})

	service := NewKnowledgeBaseService(repository, repository)
	if err := service.UpdateSourceDeletePolicy(context.Background(), workspaceID, kbID, value.SourceDeleteRemove); err != nil {
		t.Fatalf("update err = %v", err)
	}
	if repository.sourcePolicyCalls != 1 {
		t.Fatalf("policy updater calls = %d, want 1", repository.sourcePolicyCalls)
	}

	kb := repository.items[kbID]
	if kb.SourceConfig["on_delete"] != "remove" {
		t.Fatalf("on_delete = %v, want remove", kb.SourceConfig["on_delete"])
	}
	if kb.SourceConfig["root_token"] != "wikcn-root" {
		t.Fatalf("root_token = %v, want wikcn-root", kb.SourceConfig["root_token"])
	}
	if kb.SourceConfig["sync_cursor"] != "2026-08-07T00:00:00Z" {
		t.Fatalf("sync_cursor = %v", kb.SourceConfig["sync_cursor"])
	}
	if kb.SourceConfig["cron"] != "0 * * * *" {
		t.Fatalf("cron = %v", kb.SourceConfig["cron"])
	}
}

// TestUpdateSourceDeletePolicyRejectsNonFeishuKB 验证非飞书来源知识库返回 ErrValidation。
func TestUpdateSourceDeletePolicyRejectsNonFeishuKB(t *testing.T) {
	workspaceID := uuid.New()
	repository := newFakeKnowledgeBaseRepository()
	resolved := fakeResolvedEmbeddingModel(t, value.ModelScopePlatform, nil, value.ModelStatusActive, value.ModelStatusActive)
	repository.models[resolved.Model.ID] = resolved
	service := NewKnowledgeBaseService(repository, repository)
	created, err := service.Create(context.Background(), CreateKnowledgeBaseInput{
		WorkspaceID: workspaceID, Name: "上传库", EmbeddingModelID: resolved.Model.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	err = service.UpdateSourceDeletePolicy(context.Background(), workspaceID, created.ID, value.SourceDeleteRemove)
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if repository.sourcePolicyCalls != 0 {
		t.Fatalf("policy updater should not be called for non-feishu KB; calls = %d", repository.sourcePolicyCalls)
	}
}

// TestUpdateSourceDeletePolicyRejectsUnknownKB 验证未知知识库返回 ErrNotFound。
func TestUpdateSourceDeletePolicyRejectsUnknownKB(t *testing.T) {
	workspaceID := uuid.New()
	repository := newFakeKnowledgeBaseRepository()
	// 在 GetResolved 路径返回 NotFound：fake 不会命中任何 KB。
	service := NewKnowledgeBaseService(repository, repository)

	err := service.UpdateSourceDeletePolicy(context.Background(), workspaceID, uuid.New(), value.SourceDeleteRemove)
	if !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// TestUpdateSourceDeletePolicyRejectsNilIDs 验证 lineage 无效返回 ErrValidation。
func TestUpdateSourceDeletePolicyRejectsNilIDs(t *testing.T) {
	repository := newFakeKnowledgeBaseRepository()
	service := NewKnowledgeBaseService(repository, repository)

	if err := service.UpdateSourceDeletePolicy(context.Background(), uuid.Nil, uuid.New(), value.SourceDeleteKeep); !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("nil workspace err = %v, want ErrValidation", err)
	}
	if err := service.UpdateSourceDeletePolicy(context.Background(), uuid.New(), uuid.Nil, value.SourceDeleteKeep); !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("nil kb err = %v, want ErrValidation", err)
	}
}
