//go:build integration

package db

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// TestKnowledgeBaseRepositoryUpdateSourceDeletePolicyPreservesRuntimeKeys 验证
// jsonb_set 只改 on_delete，保留 root_token/sync_cursor/cron/next_sync_at 等运行期键。
func TestKnowledgeBaseRepositoryUpdateSourceDeletePolicyPreservesRuntimeKeys(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	// 把该 KB 标记为飞书来源，并写入一组运行期来源配置（含旧 on_delete=keep）。
	initial := `{"root_token":"wikcn-root","sync_cursor":"2026-08-07T00:00:00Z","cron":"0 * * * *","on_delete":"keep"}`
	if err := database.WithContext(ctx).Exec(
		"UPDATE knowledge_bases SET source_type = 'feishu_wiki', source_config = ?::jsonb WHERE id = ?",
		initial, seed.kbID,
	).Error; err != nil {
		t.Fatalf("seed feishu source_config: %v", err)
	}

	repository := NewKnowledgeBaseRepository(database)
	if err := repository.UpdateSourceDeletePolicy(ctx, seed.workspaceID, seed.kbID, value.SourceDeleteRemove); err != nil {
		t.Fatalf("UpdateSourceDeletePolicy: %v", err)
	}

	var row KnowledgeBaseRow
	if err := database.WithContext(ctx).First(&row, "id = ?", seed.kbID).Error; err != nil {
		t.Fatal(err)
	}
	cfg := row.SourceConfig
	if cfg["on_delete"] != "remove" {
		t.Fatalf("on_delete = %v, want remove", cfg["on_delete"])
	}
	if cfg["root_token"] != "wikcn-root" {
		t.Fatalf("root_token = %v, want wikcn-root", cfg["root_token"])
	}
	if cfg["sync_cursor"] != "2026-08-07T00:00:00Z" {
		t.Fatalf("sync_cursor = %v", cfg["sync_cursor"])
	}
	if cfg["cron"] != "0 * * * *" {
		t.Fatalf("cron = %v", cfg["cron"])
	}
}

// TestKnowledgeBaseRepositoryUpdateSourceDeletePolicyCreatesKeyWhenMissing 验证
// jsonb_set(..., true) 在 on_delete 缺失时自动创建。
func TestKnowledgeBaseRepositoryUpdateSourceDeletePolicyCreatesKeyWhenMissing(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	if err := database.WithContext(ctx).Exec(
		"UPDATE knowledge_bases SET source_type = 'feishu_drive', source_config = '{\"root_token\":\"tok\"}'::jsonb WHERE id = ?",
		seed.kbID,
	).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	repository := NewKnowledgeBaseRepository(database)
	if err := repository.UpdateSourceDeletePolicy(ctx, seed.workspaceID, seed.kbID, value.SourceDeleteKeep); err != nil {
		t.Fatalf("UpdateSourceDeletePolicy: %v", err)
	}

	var row KnowledgeBaseRow
	if err := database.WithContext(ctx).First(&row, "id = ?", seed.kbID).Error; err != nil {
		t.Fatal(err)
	}
	if row.SourceConfig["on_delete"] != "keep" {
		t.Fatalf("on_delete = %v, want keep", row.SourceConfig["on_delete"])
	}
	if row.SourceConfig["root_token"] != "tok" {
		t.Fatalf("root_token = %v, want tok", row.SourceConfig["root_token"])
	}
}

// TestKnowledgeBaseRepositoryUpdateSourceDeletePolicyRejectsCrossWorkspace 验证
// 跨 workspace 更新返回 ErrNotFound。
func TestKnowledgeBaseRepositoryUpdateSourceDeletePolicyRejectsCrossWorkspace(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	otherWorkspace := createWorkspaceRow(t, ctx, database, "kb-policy-xws-"+uuid.NewString())

	repository := NewKnowledgeBaseRepository(database)
	err := repository.UpdateSourceDeletePolicy(ctx, otherWorkspace, seed.kbID, value.SourceDeleteRemove)
	if !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("cross-workspace err = %v, want ErrNotFound", err)
	}
}
