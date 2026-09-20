//go:build integration

package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// 本文件覆盖 kb_sources CRUD 仓储的 PG 集成（v1.3.0 spec §9.2/§12.3）：
// 源行往返（config/运行态列）、同库活源名称唯一（uq_kb_sources_active_name 软删释放）、
// 软删语义、页面计数聚合与 keep 分支断链。

func TestKbSourceRepositoryCRUDFlow(t *testing.T) {
	// 事务夹具：knowledge_bases 前向复合 FK 为 DEFERRABLE，须在同一事务内完成种子
	//（回滚即清理，延迟约束不会在语句级提交时触发）。
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	repo := NewKbSourceRepository(database)

	source, err := model.NewKbSource(model.NewKbSourceInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		Kind: value.KbSourceKindWeb, Name: "文档站",
		Config: map[string]any{
			"mode": "urls",
			"urls": []any{"https://example.com/a"},
			"crawl": map[string]any{
				"respect_robots": false,
				"max_pages":      float64(20),
			},
			"on_delete": "remove",
		},
	})
	if err != nil {
		t.Fatalf("NewKbSource: %v", err)
	}
	if err := repo.Create(ctx, source); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// 往返：config 与运行态缺省列。
	got, err := repo.Get(ctx, seed.workspaceID, seed.kbID, source.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "文档站" || got.Status != value.KbSourceStatusActive {
		t.Fatalf("round-trip = %q/%q", got.Name, got.Status)
	}
	if _, ok := got.Config["urls"]; !ok {
		t.Fatalf("config urls 丢失: %#v", got.Config)
	}

	// CountByKB。
	if count, err := repo.CountByKB(ctx, seed.workspaceID, seed.kbID); err != nil || count != 1 {
		t.Fatalf("CountByKB = %d, %v", count, err)
	}

	// 同库活源名称唯一：同名创建 → ErrConflict。唯一冲突会中止 PG 事务，
	// 两个负向用例各放在嵌套事务（SAVEPOINT）内，保持外层事务可用。
	assertCreateConflict := func(name string) {
		t.Helper()
		duplicate, err := model.NewKbSource(model.NewKbSourceInput{
			WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
			Kind: value.KbSourceKindWeb, Name: name, Config: map[string]any{},
		})
		if err != nil {
			t.Fatalf("NewKbSource(%q): %v", name, err)
		}
		if err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return NewKbSourceRepository(tx).Create(ctx, duplicate)
		}); !errors.Is(err, domainerrors.ErrConflict) {
			t.Fatalf("duplicate name %q error = %v, want ErrConflict", name, err)
		}
	}
	assertCreateConflict("文档站")
	// trim 归一后同名同样冲突（模型层 trim + 索引按 lower(name)）。
	assertCreateConflict("文档站 ")

	// Update：改名 + 停用 + config 更新；不触碰运行态列。
	// 先模拟并发 worker 已推进运行态（latch 置位），再带着过期运行态快照 Update。
	if err := database.WithContext(ctx).Exec(
		"UPDATE kb_sources SET sync_requested_force = true WHERE id = ?", source.ID,
	).Error; err != nil {
		t.Fatalf("simulate latch: %v", err)
	}
	got.Status = value.KbSourceStatusDisabled
	got.Name = "文档站改名"
	got.Config = map[string]any{"mode": "urls", "urls": []any{"https://example.com/b"}}
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	reread, err := repo.Get(ctx, seed.workspaceID, seed.kbID, source.ID)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if reread.Name != "文档站改名" || reread.Status != value.KbSourceStatusDisabled {
		t.Fatalf("after update = %q/%q", reread.Name, reread.Status)
	}
	if !reread.SyncRequestedForce {
		t.Fatal("Update 不应覆盖 sync_requested_force 运行态列")
	}

	// List：跨 workspace/KB 不可见。
	others, err := repo.List(ctx, seed.workspaceID, uuid.New())
	if err != nil || len(others) != 0 {
		t.Fatalf("List(other kb) = %d, %v", len(others), err)
	}

	// 软删：Get/List 拒绝，名字释放可重建。
	if err := repo.SoftDelete(ctx, seed.workspaceID, seed.kbID, source.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	if _, err := repo.Get(ctx, seed.workspaceID, seed.kbID, source.ID); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("Get after delete error = %v, want ErrNotFound", err)
	}
	if err := repo.SoftDelete(ctx, seed.workspaceID, seed.kbID, source.ID); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("重复软删 error = %v, want ErrNotFound", err)
	}
	recreated, err := model.NewKbSource(model.NewKbSourceInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		Kind: value.KbSourceKindWeb, Name: "文档站改名", Config: map[string]any{},
	})
	if err != nil {
		t.Fatalf("NewKbSource(recreate): %v", err)
	}
	if err := repo.Create(ctx, recreated); err != nil {
		t.Fatalf("软删后同名重建失败（唯一索引未释放名字）: %v", err)
	}
}

func TestKbSourcePageCountsAndDetach(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	repo := NewKbSourceRepository(database)

	sourceA := insertWebSource(t, ctx, database, seed, "源A", map[string]any{"mode": "urls", "urls": []any{}})
	sourceB := insertWebSource(t, ctx, database, seed, "源B", map[string]any{"mode": "urls", "urls": []any{}})

	// 源A：2 个存活页面（1 failed）+ 1 个软删页面；源B：1 个存活页面。
	insertWebPageDocument(t, ctx, database, seed, sourceA.ID, "页面1", value.DocumentStatusReady, nil)
	insertWebPageDocument(t, ctx, database, seed, sourceA.ID, "页面2", value.DocumentStatusFailed, nil)
	insertWebPageDocument(t, ctx, database, seed, sourceA.ID, "页面3", value.DocumentStatusReady, ptrTimeNowPtr())
	insertWebPageDocument(t, ctx, database, seed, sourceB.ID, "页面4", value.DocumentStatusReady, nil)

	counts, err := repo.CountPagesBySource(ctx, seed.workspaceID, seed.kbID)
	if err != nil {
		t.Fatalf("CountPagesBySource: %v", err)
	}
	if counts[sourceA.ID] != (appservice.SourcePageCounts{PageCount: 2, FailedPageCount: 1}) {
		t.Fatalf("源A counts = %+v, want {2 1}", counts[sourceA.ID])
	}
	if counts[sourceB.ID] != (appservice.SourcePageCounts{PageCount: 1, FailedPageCount: 0}) {
		t.Fatalf("源B counts = %+v, want {1 0}", counts[sourceB.ID])
	}

	ids, err := repo.ListDocumentIDsBySource(ctx, seed.workspaceID, seed.kbID, sourceA.ID)
	if err != nil {
		t.Fatalf("ListDocumentIDsBySource: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("源A 文档数 = %d, want 2（软删行不计）", len(ids))
	}

	// keep 分支断链：源A 全部页面 kb_source_id 置 NULL（含软删行），源B 不受影响。
	detached, err := repo.DetachSourceDocuments(ctx, seed.workspaceID, seed.kbID, sourceA.ID)
	if err != nil {
		t.Fatalf("DetachSourceDocuments: %v", err)
	}
	if detached != 3 {
		t.Fatalf("断链行数 = %d, want 3（含软删行）", detached)
	}
	counts, err = repo.CountPagesBySource(ctx, seed.workspaceID, seed.kbID)
	if err != nil {
		t.Fatalf("CountPagesBySource after detach: %v", err)
	}
	if _, ok := counts[sourceA.ID]; ok {
		t.Fatal("源A 断链后不应再有页面计数")
	}
	if counts[sourceB.ID].PageCount != 1 {
		t.Fatalf("源B 计数被误断链: %+v", counts[sourceB.ID])
	}

	// 跨 KB 隔离：另一 KB 下断链为 0。
	otherKBDetached, err := repo.DetachSourceDocuments(ctx, seed.workspaceID, uuid.New(), sourceA.ID)
	if err != nil || otherKBDetached != 0 {
		t.Fatalf("跨 KB 断链 = %d, %v; want 0, nil", otherKBDetached, err)
	}
}

// insertWebPageDocument 直接落库一个 kind=web 页面文档（kb_source_id 归属 + 可选软删）。
func insertWebPageDocument(
	t *testing.T,
	ctx context.Context,
	tx *gorm.DB,
	seed knowledgeSchemaSeed,
	sourceID uuid.UUID,
	title string,
	status value.DocumentStatus,
	deletedAt *time.Time,
) {
	t.Helper()
	uri := "https://example.com/" + strings.ToLower(title) + "-" + uuid.NewString()[:8]
	if err := tx.WithContext(ctx).Exec(
		"INSERT INTO documents "+
			"(id, workspace_id, knowledge_base_id, kind, title, source_type, source_uri, status, kb_source_id, source_meta, deleted_at) "+
			"VALUES (?, ?, ?, 'web', ?, 'web', ?, ?, ?, '{}', ?)",
		uuid.New(), seed.workspaceID, seed.kbID, title, uri, string(status), sourceID, deletedAt,
	).Error; err != nil {
		t.Fatalf("insert web page document: %v", err)
	}
}

func ptrTimeNowPtr() *time.Time {
	now := time.Now().UTC()
	return &now
}
