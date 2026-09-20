//go:build integration

package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/dajee/langhuan/internal/application/dto"
	appservice "github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
	"github.com/dajee/langhuan/internal/infrastructure/db"
	"github.com/dajee/langhuan/internal/testsupport"
)

// 来源同步 parse Job 的 lineage 闭环（评审补口）：
//
//	同步建文档 → 内容更新产生新 revision/parse Job → 再同步重试（回填/新建）
//	每一步落库的 Job payload 都必须携带 index_generation_id，且 worker 的
//	validateDocumentTaskLineage 能用真实 DB 行通过校验。此前只有 service 层
//	混装 fake 的测试，绕过了 worker 校验，正是 update/retry 路径漏网的盲区。
func TestSourceSyncParseJobPayloadLineageIntegration(t *testing.T) {
	ctx := context.Background()
	testDSN := testsupport.NewMigratedPostgres(t)
	gormDB, _, err := db.Open(config.DatabaseConfig{Driver: "postgres", DSN: testDSN})
	if err != nil {
		t.Fatal(err)
	}

	// KB 与 generation 互相外键（DEFERRABLE INITIALLY DEFERRED，提交期校验）：
	// 全部种子与写入放在单事务内，事务回滚即清理。
	tx := gormDB.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	gormDB = tx

	// —— 最小 schema 种子：workspace/user/provider/model/kb/generation/root ——
	seed := seedParseLineageSchema(t, ctx, gormDB)
	store := db.NewSourceSyncDBStore(gormDB)
	jobRepo := db.NewJobRepository(gormDB)
	revisionRepo := db.NewDocumentRevisionRepository(gormDB)

	// —— 同步（add 路径建档，与 service 一致：Job payload 带 generation）——
	document, _, _, addJob := seedSyncedParseLineageDocument(t, ctx, gormDB, store, seed)
	assertParseJobPayloadHasGeneration(t, ctx, gormDB, seed.workspaceID, addJob.ID, seed.generationID)

	// —— 内容更新：CreateSyncedDocumentRevisionJob 新 revision + parse Job ——
	updatedRevisionID := uuid.New()
	updateResult, err := store.CreateSyncedDocumentRevisionJob(ctx, appservice.UpdateDocumentRequest{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		ExternalID: "lineage-doc", DocumentID: document.ID, RevisionID: updatedRevisionID,
		GenerationID: seed.generationID,
		Title:        "lineage-doc（已更新）", ParentNodeID: seed.rootID,
		RawStorageKey: "raw/lineage-v2.md", SHA256: "hash-v2",
		SizeBytes: 12, ContentType: "text/markdown", FileType: "markdown",
		Reason: value.DocumentRevisionReasonCrawl,
	})
	if err != nil {
		t.Fatalf("CreateSyncedDocumentRevisionJob: %v", err)
	}
	assertParseJobPayloadHasGeneration(t, ctx, gormDB, seed.workspaceID, updateResult.JobID, seed.generationID)
	assertWorkerLineageAccepts(t, ctx, jobRepo, revisionRepo, seed, updateResult.JobID)

	// 队列 payload 缺 generation 时（异常生产者），worker 必须拒绝——防校验空转。
	brokenPayload := DocumentTaskPayload{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		DocumentID: document.ID, DocumentRevisionID: updatedRevisionID,
		JobID: updateResult.JobID,
	}
	job, revision := loadLineageInputs(t, ctx, jobRepo, revisionRepo, seed.workspaceID, updateResult.JobID)
	if err := validateDocumentTaskLineage("document_parse_start", brokenPayload, job, revision); err == nil {
		t.Fatal("payload 缺 index_generation_id 时 lineage 校验应失败")
	} else if !strings.Contains(err.Error(), "Job payload 与任务 Generation lineage 不一致") {
		t.Fatalf("lineage 错误文案不符: %v", err)
	}

	// —— 再同步（重试·回填分支）：模拟历史遗留 payload 缺键，重试须回填 ——
	if err := gormDB.WithContext(ctx).Exec(
		"UPDATE jobs SET payload = '{}'::jsonb WHERE workspace_id = ? AND id = ?",
		seed.workspaceID, updateResult.JobID,
	).Error; err != nil {
		t.Fatalf("清空 payload 模拟遗留 Job: %v", err)
	}
	retryResult, err := store.RetrySourceRevision(ctx, appservice.RetryDocumentRequest{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		DocumentID: document.ID, RevisionID: updatedRevisionID,
		GenerationID: seed.generationID,
		SHA256:       "hash-v2", Title: "lineage-doc（已更新）", ParentNodeID: seed.rootID,
	})
	if err != nil {
		t.Fatalf("RetrySourceRevision（回填分支）: %v", err)
	}
	if retryResult.JobID != updateResult.JobID {
		t.Fatalf("重试应复用既有 Job %s, got %s", updateResult.JobID, retryResult.JobID)
	}
	assertParseJobPayloadHasGeneration(t, ctx, gormDB, seed.workspaceID, retryResult.JobID, seed.generationID)
	assertWorkerLineageAccepts(t, ctx, jobRepo, revisionRepo, seed, retryResult.JobID)

	// —— 再同步（重试·新建分支）：删除既有 Job 后重试，新 Job 同样带 generation ——
	if err := gormDB.WithContext(ctx).Exec(
		"DELETE FROM jobs WHERE workspace_id = ? AND id = ?",
		seed.workspaceID, retryResult.JobID,
	).Error; err != nil {
		t.Fatalf("删除既有 parse Job: %v", err)
	}
	rebuildResult, err := store.RetrySourceRevision(ctx, appservice.RetryDocumentRequest{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		DocumentID: document.ID, RevisionID: updatedRevisionID,
		GenerationID: seed.generationID,
		SHA256:       "hash-v2", Title: "lineage-doc（已更新）", ParentNodeID: seed.rootID,
	})
	if err != nil {
		t.Fatalf("RetrySourceRevision（新建分支）: %v", err)
	}
	if rebuildResult.JobID == retryResult.JobID {
		t.Fatalf("新建分支应产出新 Job, got 复用 %s", rebuildResult.JobID)
	}
	assertParseJobPayloadHasGeneration(t, ctx, gormDB, seed.workspaceID, rebuildResult.JobID, seed.generationID)
	assertWorkerLineageAccepts(t, ctx, jobRepo, revisionRepo, seed, rebuildResult.JobID)
}

// parseLineageSeed 是 parse Job lineage 集成测试的最小 schema 种子。
type parseLineageSeed struct {
	workspaceID  uuid.UUID
	kbID         uuid.UUID
	rootID       uuid.UUID
	generationID uuid.UUID
}

// seedParseLineageSchema 建齐 parse Job 外键链路所需的最小行集
// （workspaces/users/model_providers/models/knowledge_bases/generation/root node）。
func seedParseLineageSchema(t *testing.T, ctx context.Context, gormDB *gorm.DB) parseLineageSeed {
	t.Helper()
	seed := parseLineageSeed{
		workspaceID: uuid.New(), kbID: uuid.New(),
		rootID: uuid.New(), generationID: uuid.New(),
	}
	userID, providerID, modelID := uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC().Add(-time.Hour)
	statements := []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO workspaces (id, name, slug) VALUES (?, 'parse-lineage', ?)",
			[]any{seed.workspaceID, "parse-lineage-" + uuid.NewString()}},
		{"INSERT INTO users (id, email, nickname, password_hash) VALUES (?, ?, 'parse-lineage', 'hash')",
			[]any{userID, uuid.NewString() + "@example.com"}},
		{"INSERT INTO model_providers (id, scope, workspace_id, name, provider, status, created_by) " +
			"VALUES (?, 'workspace', ?, ?, 'openai', 'active', ?)",
			[]any{providerID, seed.workspaceID, "provider-" + uuid.NewString(), userID}},
		{"INSERT INTO models (id, provider_id, name, type, model_name, dimensions, status, created_by) " +
			"VALUES (?, ?, ?, 'embedding', 'text-embedding', 1024, 'active', ?)",
			[]any{modelID, providerID, "model-" + uuid.NewString(), userID}},
		// KB 与 generation 互相外键（可延迟约束，事务内允许循环引用插入）：
		// KB 行携带 active_index_generation_id 与 file_tree_root_id，随后补
		// generation 行与 root 节点行，事务提交期统一校验。
		{"INSERT INTO knowledge_bases (id, workspace_id, name, active_index_generation_id, file_tree_root_id) " +
			"VALUES (?, ?, ?, ?, ?)",
			[]any{seed.kbID, seed.workspaceID, "kb-" + uuid.NewString(), seed.generationID, seed.rootID}},
		{"INSERT INTO knowledge_base_index_generations " +
			"(id, workspace_id, knowledge_base_id, embedding_model_id, provider_id, model_name, " +
			"embedding_dimension, model_config_hash, chunker_version, chunking_config, config_hash, status, created_at) " +
			"VALUES (?, ?, ?, ?, ?, 'text-embedding', 1024, 'model-hash', 1, " +
			"'{\"chunk_size\":512,\"chunk_overlap\":80}'::jsonb, 'config-hash', 'ready', ?)",
			[]any{seed.generationID, seed.workspaceID, seed.kbID, modelID, providerID, now}},
		{"INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, node_type, name) " +
			"VALUES (?, ?, ?, 'root', '')",
			[]any{seed.rootID, seed.workspaceID, seed.kbID}},
	}
	for _, statement := range statements {
		if err := gormDB.WithContext(ctx).Exec(statement.sql, statement.args...).Error; err != nil {
			t.Fatalf("seed parse lineage schema: %v", err)
		}
	}
	return seed
}

// seedSyncedParseLineageDocument 走 add 路径（CreateSyncedDocumentNodeRevisionAndJob）
// 建一个已归属源的同步文档，Job payload 与生产 service 同构（带 generation）。
func seedSyncedParseLineageDocument(
	t *testing.T, ctx context.Context, gormDB *gorm.DB,
	store *db.SourceSyncDBStore, seed parseLineageSeed,
) (*model.Document, *model.DocumentRevision, *model.FileTreeNode, *model.Job) {
	t.Helper()
	document, err := model.NewDocumentIdentityWithExternal(
		seed.workspaceID, seed.kbID, value.DocumentKindFile, "lineage-doc",
		model.SourceProviderFeishu, "", "lineage-doc", nil,
	)
	if err != nil {
		t.Fatalf("new lineage document: %v", err)
	}
	revision, err := model.NewDocumentRevision(model.NewDocumentRevisionInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID, DocumentID: document.ID,
		Kind: value.DocumentKindFile, DocumentKind: value.DocumentKindFile,
		RevisionNo: 1, Reason: value.DocumentRevisionReasonCrawl,
		OriginalFilename: "lineage-doc.md", FileType: "markdown", ContentType: "text/markdown",
		RawStorageKey: "raw/lineage-v1.md", SHA256: "hash-v1",
		SizeBytes: 7, ProcessingVersion: model.CurrentProcessingVersion,
		Status: value.DocumentRevisionPending,
	})
	if err != nil {
		t.Fatalf("new lineage revision: %v", err)
	}
	documentID := document.ID
	node, err := model.NewFileTreeNode(model.NewFileTreeNodeInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID, ParentID: &seed.rootID,
		NodeType: value.FileTreeNodeFile, Name: "lineage-doc",
		DocumentID: &documentID, DocumentKind: value.DocumentKindFile,
	})
	if err != nil {
		t.Fatalf("new lineage file node: %v", err)
	}
	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		DocumentID: document.ID, DocumentRevisionID: revision.ID,
		Type: "document_parse_start", Status: value.JobStatusPending,
	})
	if err != nil {
		t.Fatalf("new lineage parse job: %v", err)
	}
	job.Payload["index_generation_id"] = seed.generationID.String()
	if err := store.WithinWorkspace(
		ctx, seed.workspaceID,
		func(txCtx context.Context, tx appservice.SourceSyncTx) error {
			return tx.CreateSyncedDocumentNodeRevisionAndJob(txCtx, document, node, revision, job)
		},
	); err != nil {
		t.Fatalf("seed lineage synced document: %v", err)
	}
	return document, revision, node, job
}

// assertParseJobPayloadHasGeneration 断言 DB 中 Job payload 携带指定 generation。
func assertParseJobPayloadHasGeneration(
	t *testing.T, ctx context.Context, gormDB *gorm.DB, workspaceID, jobID, generationID uuid.UUID,
) {
	t.Helper()
	var row struct {
		Payload db.JSONMap
	}
	if err := gormDB.WithContext(ctx).Raw(
		"SELECT payload FROM jobs WHERE workspace_id = ? AND id = ?",
		workspaceID, jobID,
	).Scan(&row).Error; err != nil {
		t.Fatalf("read parse job payload: %v", err)
	}
	if got := row.Payload["index_generation_id"]; got != generationID.String() {
		t.Fatalf("job payload index_generation_id = %#v, want %s", got, generationID)
	}
}

// loadLineageInputs 从 DB 加载 worker 校验所需的 Job/Revision 领域对象。
func loadLineageInputs(
	t *testing.T, ctx context.Context,
	jobRepo *db.JobRepository, revisionRepo *db.DocumentRevisionRepository,
	workspaceID, jobID uuid.UUID,
) (*dto.Job, *model.DocumentRevision) {
	t.Helper()
	modelJob, err := jobRepo.Get(ctx, workspaceID, jobID)
	if err != nil {
		t.Fatalf("load job: %v", err)
	}
	revision, err := revisionRepo.Get(ctx, workspaceID, modelJob.DocumentRevisionID)
	if err != nil {
		t.Fatalf("load revision: %v", err)
	}
	return dto.JobFromModel(modelJob), revision
}

// assertWorkerLineageAccepts 用 DB 真实行跑 worker 的 lineage 校验（队列 payload
// 与生产 enqueueParseJob 同构：generation_id 即目标代次）。
func assertWorkerLineageAccepts(
	t *testing.T, ctx context.Context,
	jobRepo *db.JobRepository, revisionRepo *db.DocumentRevisionRepository,
	seed parseLineageSeed, jobID uuid.UUID,
) {
	t.Helper()
	job, revision := loadLineageInputs(t, ctx, jobRepo, revisionRepo, seed.workspaceID, jobID)
	payload := DocumentTaskPayload{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		DocumentID: job.DocumentID, DocumentRevisionID: job.DocumentRevisionID,
		GenerationID: seed.generationID, JobID: job.ID,
	}
	if err := validateDocumentTaskLineage("document_parse_start", payload, job, revision); err != nil {
		t.Fatalf("worker lineage 校验应通过: %v", err)
	}
}
