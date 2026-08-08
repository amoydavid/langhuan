package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	sourceport "github.com/dajee/langhuan/internal/ports/source"
)

// 本文件聚焦 spec 12.2/12.3 中尚未被既有测试覆盖的健壮性回归场景。
// 既有覆盖（不重复）：
//   - 单节点失败继续处理（无 partial 断言）：TestSyncContinuesOnSingleFetchError
//   - fatal ListTree 写 failed 结果：TestSyncFatalListErrorWritesFailedResult
//   - fatal ListTree → RunSourceSyncJob 标 failed：TestRunSourceSyncJobMarksFailedOnSyncError
//   - worker 不直接标终态：TestSourceSyncHandleForwardsLineageAndMarksRunning
//   - diff 层"已软删+远端重现=>ToUpdate"：TestDiffRules
//   - 单层空 folder 删除：TestCompleteSnapshotDeletesEmptyMissingFolder

// TestOneNodeFailureContinuesAndWritesPartialResult（spec 12.2）验证：
// 单个节点 Fetch 失败不阻塞其它节点（good-doc 仍被同步），但整体结果为 partial；
// 失败节点不被落库，成功节点被落库；两个节点都被 Fetch（确认失败不短路）。
func TestOneNodeFailureContinuesAndWritesPartialResult(t *testing.T) {
	env := newSourceSyncServiceTestEnv(t)
	// 两个 docx 节点：bad-doc 永久 Fetch 失败，good-doc 成功。
	env.snapshotWithDocs(
		fakeDoc{token: "good-doc", title: "好文档", markdown: "# good"},
		fakeDoc{token: "bad-doc", title: "坏文档", markdown: "# bad"},
	)
	env.connector.fetchErrByToken = map[string]error{"bad-doc": errors.New("feishu 503")}

	if err := env.syncOnceErr(t); err != nil {
		t.Fatalf("单节点失败不应让 SyncKnowledgeBase 返回 error; got %v", err)
	}

	// 结果应为 partial（有 failed 节点）。
	res := env.syncResult()
	if res == nil || res.Status != "partial" {
		t.Fatalf("SyncResult.Status = %v, want partial; res=%#v", res, res)
	}
	if res.FailedDocuments != 1 {
		t.Fatalf("FailedDocuments = %d, want 1", res.FailedDocuments)
	}
	if res.SyncedDocuments != 1 {
		t.Fatalf("SyncedDocuments = %d, want 1", res.SyncedDocuments)
	}

	// good-doc 被落库，bad-doc 未被落库。
	if doc := env.documentByExternalID("good-doc"); doc == nil {
		t.Fatal("good-doc 应被同步落库")
	}
	if doc := env.documentByExternalID("bad-doc"); doc != nil {
		t.Fatalf("bad-doc 不应被落库; got %#v", doc)
	}

	// 两个节点都被 Fetch（失败不短路其它节点）。
	counts := env.fetchedTokenCount()
	if counts["good-doc"] != 1 {
		t.Fatalf("good-doc 应被 Fetch 一次; got %d", counts["good-doc"])
	}
	if counts["bad-doc"] != 1 {
		t.Fatalf("bad-doc 应被 Fetch 一次（失败也应被尝试）; got %d", counts["bad-doc"])
	}
}

// TestFatalListTreeUsesSourceUnavailableSentinel（spec 12.2/3.1）验证：ListTree 返回
// fatal 错误（ErrSourceUnavailable）时，SyncKnowledgeBase 返回 error 且写 status=failed
// 的 SyncResult。补强既有 TestSyncFatalListErrorWritesFailedResult（用真实 sentinel）。
func TestFatalListTreeUsesSourceUnavailableSentinel(t *testing.T) {
	env := newSourceSyncServiceTestEnv(t)
	env.connector.listErr = sourceport.ErrSourceUnavailable

	err := env.syncOnceErr(t)
	if err == nil {
		t.Fatal("ListTree 致命错误应返回 error")
	}
	if !errors.Is(err, sourceport.ErrSourceUnavailable) {
		t.Fatalf("error 链应包含 ErrSourceUnavailable; got %v", err)
	}
	res := env.syncResult()
	if res == nil || res.Status != "failed" {
		t.Fatalf("应写 failed SyncResult; got %#v", res)
	}
	if res.Complete {
		t.Fatalf("fatal 结果 Complete 应为 false; got %#v", res)
	}
}

// TestSourceSyncDoesNotInvokeParserDirectly（spec 12.2）验证 source sync 不直接调用
// parser/embedding/index 适配器，只通过队列入队 document_parse_start 让现有 parse pipeline 接手。
// 断言分两层：
//  1. 结构层：SourceSyncServiceDeps 不含 parser/embedding/index 字段；
//  2. 行为层：一次完整同步后，fake 队列里只出现 document_parse_start 任务类型。
func TestSourceSyncDoesNotInvokeParserDirectly(t *testing.T) {
	// 结构层：枚举 SourceSyncServiceDeps 字段名，禁止出现 parser/embedding/index 相关依赖。
	depsType := reflect.TypeOf(SourceSyncServiceDeps{})
	for i := 0; i < depsType.NumField(); i++ {
		name := depsType.Field(i).Name
		for _, banned := range []string{"Parser", "Embedding", "Index", "Chunker", "Retrieval"} {
			if name == banned {
				t.Fatalf("SourceSyncServiceDeps 不应持有 %q 字段（source sync 不得直接调用 parse/embed/index）", name)
			}
		}
	}

	// 行为层：一次完整同步后，fake 队列只入队 document_parse_start。
	env := newSourceSyncServiceTestEnv(t)
	env.snapshotWithDocs(
		fakeDoc{token: "docA", title: "A", markdown: "# A"},
		fakeDoc{token: "docB", title: "B", markdown: "# B"},
	)
	env.syncOnce(t)

	if len(env.queue.requests) == 0 {
		t.Fatal("应至少入队一个 parse 任务")
	}
	for i, req := range env.queue.requests {
		if req.Type != documentParseStartJobType {
			t.Fatalf("queue.requests[%d].Type = %q, want %q（source sync 只能入队 parse 任务）",
				i, req.Type, documentParseStartJobType)
		}
	}
}

// TestDeletedDocumentReappearingIsRestored（spec 9.3/12.2）验证：已软删的文档在远端重新
// 出现时，同步会复用原 Document 身份、清空 deleted_at、创建新 revision（或复用就绪 revision）。
// diff 层的分类已由 TestDiffRules 覆盖；这里覆盖服务层的真实落库恢复语义。
func TestDeletedDocumentReappearingIsRestored(t *testing.T) {
	env := newSourceSyncServiceTestEnv(t)
	// 第一次同步：建立 doc-1。
	env.snapshotWithDoc("doc-1", "v1")
	env.syncOnce(t)

	doc := env.documentByExternalID("doc-1")
	if doc == nil {
		t.Fatal("doc-1 未创建")
	}
	// 模拟后续被删除（软删）。
	deletedAt := pastTime()
	doc.DeletedAt = &deletedAt
	doc.Status = value.DocumentStatusDeleted
	beforeRevisions := env.revisionCount(doc.ID)

	// 远端重新出现 doc-1（内容变化），同步应恢复（清 deleted_at）并创建新 revision。
	env.snapshotWithDoc("doc-1", "v2-resurrected")
	env.syncOnce(t)

	restored := env.documentByExternalID("doc-1")
	if restored == nil {
		t.Fatal("doc-1 应仍存在")
	}
	if restored.DeletedAt != nil {
		t.Fatalf("恢复后 deleted_at 应为 nil; got %v", restored.DeletedAt)
	}
	if got := env.revisionCount(doc.ID); got != beforeRevisions+1 {
		t.Fatalf("恢复应创建新 revision; got %d want %d", got, beforeRevisions+1)
	}
}

// TestNestedFolderDeepDeleteClearsLeafFolders（spec 5.2/12.2）验证完整 snapshot 删除失踪
// folder 时按深度从深到浅处理：叶子 folder 被删除，仍含子节点的 folder 被保留。
// 既有 TestCompleteSnapshotDeletesEmptyMissingFolder 只覆盖单层平铺 folder。
func TestNestedFolderDeepDeleteClearsLeafFolders(t *testing.T) {
	env := newSourceSyncServiceTestEnv(t)
	rootID := env.kb.FileTreeRootID

	// 预置两个独立的叶子 folder（均挂在 root 下，互为兄弟），以及一个含文档的 folder。
	leafA, err := model.NewFileTreeNode(model.NewFileTreeNodeInput{
		WorkspaceID: env.workspaceID, KnowledgeBaseID: env.kb.ID,
		ParentID: &rootID, NodeType: value.FileTreeNodeFolder,
		Name: "叶子A", ExternalID: "leafA",
	})
	if err != nil {
		t.Fatalf("new leafA: %v", err)
	}
	leafB, err := model.NewFileTreeNode(model.NewFileTreeNodeInput{
		WorkspaceID: env.workspaceID, KnowledgeBaseID: env.kb.ID,
		ParentID: &rootID, NodeType: value.FileTreeNodeFolder,
		Name: "叶子B", ExternalID: "leafB",
	})
	if err != nil {
		t.Fatalf("new leafB: %v", err)
	}
	// folderKept 仍然挂载一个 file 子节点（文档），应被保留。
	folderKept, err := model.NewFileTreeNode(model.NewFileTreeNodeInput{
		WorkspaceID: env.workspaceID, KnowledgeBaseID: env.kb.ID,
		ParentID: &rootID, NodeType: value.FileTreeNodeFolder,
		Name: "含文档目录", ExternalID: "folderKept",
	})
	if err != nil {
		t.Fatalf("new folderKept: %v", err)
	}
	keptDoc := makeExistingExternalDoc(env.workspaceID, env.kb.ID, "kept-doc", "保留文档")
	env.store.documents[keptDoc.ID] = keptDoc
	keptFile, err := model.NewFileTreeNode(model.NewFileTreeNodeInput{
		WorkspaceID: env.workspaceID, KnowledgeBaseID: env.kb.ID,
		ParentID: &folderKept.ID, NodeType: value.FileTreeNodeFile,
		Name: "保留文档", DocumentID: &keptDoc.ID, DocumentKind: value.DocumentKindFile,
		ExternalID: "kept-doc",
	})
	if err != nil {
		t.Fatalf("new keptFile: %v", err)
	}
	env.store.nodes[leafA.ID] = leafA
	env.store.nodes[leafB.ID] = leafB
	env.store.nodes[folderKept.ID] = folderKept
	env.store.nodes[keptFile.ID] = keptFile
	env.store.folderByExternal["leafA"] = leafA.ID
	env.store.folderByExternal["leafB"] = leafB.ID
	env.store.folderByExternal["folderKept"] = folderKept.ID

	// 完整 snapshot 不含任何 folder => 删除叶子 leafA/leafB；folderKept 因仍含 file 子节点而保留。
	env.connector.complete = true
	env.connector.tree = nil
	env.syncOnce(t)

	if _, stillThere := env.store.folderByExternal["leafA"]; stillThere {
		t.Fatal("完整 snapshot 应删除空叶子 leafA")
	}
	if _, stillThere := env.store.folderByExternal["leafB"]; stillThere {
		t.Fatal("完整 snapshot 应删除空叶子 leafB")
	}
	if _, stillThere := env.store.folderByExternal["folderKept"]; !stillThere {
		t.Fatal("完整 snapshot 应保留含 file 子节点的 folderKept")
	}
}

// TestNestedFolderCascadeDeleteClearsParentInOnePass（spec 5.2）验证：
// 完整 snapshot 下，当叶子 folder 被删除后，因失去子节点而变空的父 folder
// 必须在同一轮同步中级联删除（深度从深到浅），而不是等到下一次同步。
func TestNestedFolderCascadeDeleteClearsParentInOnePass(t *testing.T) {
	env := newSourceSyncServiceTestEnv(t)
	rootID := env.kb.FileTreeRootID

	// parent -> child（两层 folder），两者都远端缺失且均为空。
	parent, err := model.NewFileTreeNode(model.NewFileTreeNodeInput{
		WorkspaceID: env.workspaceID, KnowledgeBaseID: env.kb.ID,
		ParentID: &rootID, NodeType: value.FileTreeNodeFolder,
		Name: "父目录", ExternalID: "parentFolder",
	})
	if err != nil {
		t.Fatalf("new parent: %v", err)
	}
	child, err := model.NewFileTreeNode(model.NewFileTreeNodeInput{
		WorkspaceID: env.workspaceID, KnowledgeBaseID: env.kb.ID,
		ParentID: &parent.ID, NodeType: value.FileTreeNodeFolder,
		Name: "子目录", ExternalID: "childFolder",
	})
	if err != nil {
		t.Fatalf("new child: %v", err)
	}
	env.store.nodes[parent.ID] = parent
	env.store.nodes[child.ID] = child
	env.store.folderByExternal["parentFolder"] = parent.ID
	env.store.folderByExternal["childFolder"] = child.ID

	// 完整 snapshot 不含这两个 folder。
	env.connector.complete = true
	env.connector.tree = nil
	env.syncOnce(t)

	if _, stillThere := env.store.folderByExternal["childFolder"]; stillThere {
		t.Fatal("完整 snapshot 应删除叶子 childFolder")
	}
	if _, stillThere := env.store.folderByExternal["parentFolder"]; stillThere {
		t.Fatal("完整 snapshot 应在同一轮级联删除变空的 parentFolder")
	}
}

// TestRemovePolicyRecordsCleanupPending（spec 9.2/12.2）验证 remove 删除策略下，
// SyncResult.CleanupPending 统计待清理对象数，且 cleanup Job 被入队。
// 既有删除测试只覆盖 keep/remove DB 行为，未断言 SyncResult.CleanupPending。
func TestRemovePolicyRecordsCleanupPending(t *testing.T) {
	env := newSourceSyncServiceTestEnv(t)
	// 配置 KB 为 remove 策略。
	if env.kb.SourceConfig == nil {
		env.kb.SourceConfig = map[string]any{}
	}
	env.kb.SourceConfig["on_delete"] = value.SourceDeleteRemove.String()

	// 预置一个本地存在、远端将不存在的文档（触发删除）。
	seeded := makeExistingExternalDoc(env.workspaceID, env.kb.ID, "doc-gone", "已删文档")
	// 同时落库到 store.documents，让 DeleteSourceDocument 能找到它。
	env.store.documents[seeded.ID] = seeded
	env.store.existingDocuments = []*model.Document{seeded}
	// 注入 fake store 返回 2 个清理对象 + 1 个 cleanup Job。
	env.store.deleteReturnObjects = []CleanupObject{
		{Key: "raw/doc-gone.md", Store: "raw"},
		{Key: "parser/doc-gone.bin", Store: "parser"},
	}
	cleanupJob, err := model.NewJob(model.NewJobInput{
		WorkspaceID: env.workspaceID, KnowledgeBaseID: env.kb.ID,
		Type: model.SourceCleanupJobType, Status: value.JobStatusPending,
	})
	if err != nil {
		t.Fatalf("new cleanup job: %v", err)
	}
	env.store.deleteReturnJobs = []*model.Job{cleanupJob}

	// 完整 snapshot 为空 => doc-gone 被删除。
	env.connector.complete = true
	env.connector.tree = nil
	env.syncOnce(t)

	res := env.syncResult()
	if res == nil {
		t.Fatal("应写入 SyncResult")
	}
	if res.DeletedDocuments != 1 {
		t.Fatalf("DeletedDocuments = %d, want 1", res.DeletedDocuments)
	}
	if res.CleanupPending != 2 {
		t.Fatalf("CleanupPending = %d, want 2（2 个清理对象）", res.CleanupPending)
	}

	// cleanup Job 应被入队（source_cleanup 任务类型）。
	foundCleanup := false
	for _, req := range env.queue.requests {
		if req.Type == model.SourceCleanupJobType {
			foundCleanup = true
		}
	}
	if !foundCleanup {
		t.Fatal("应入队 source_cleanup 任务")
	}
}

// pastTime 返回一个确定的过去时间，用于软删标记（deleted_at 非零）。
func pastTime() time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
}

// 为编译需要：保留 context 与 sourceport 引用（部分断言使用）。
var _ = context.Background
var _ sourceport.TreeSnapshot
