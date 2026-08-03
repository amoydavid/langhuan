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
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

func TestIndexGenerationStoreCreatesListsAndActivatesAfterTreeRename(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	store := NewIndexGenerationStore(database)
	candidate := testIndexGenerationCandidate(t, seed, value.IndexGenerationBuilding)
	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		IndexGenerationID: candidate.ID, Type: "index_generation_build", Status: value.JobStatusPending,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithinWorkspace(ctx, seed.workspaceID, func(txCtx context.Context, tx appservice.IndexGenerationTx) error {
		manual, disabled, err := tx.GetActiveManualEditStats(txCtx, seed.kbID)
		if err != nil {
			return err
		}
		if manual != 0 || disabled != 0 {
			t.Fatalf("manual=%d disabled=%d", manual, disabled)
		}
		return tx.CreateIndexGeneration(txCtx, candidate, job)
	}); err != nil {
		t.Fatal(err)
	}
	items, err := store.List(ctx, seed.workspaceID, seed.kbID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != candidate.ID {
		t.Fatalf("generations = %#v", items)
	}
	var jobRow JobRow
	if err := database.WithContext(ctx).First(&jobRow, "workspace_id = ? AND id = ?", seed.workspaceID, job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if jobRow.IndexGenerationID == nil || *jobRow.IndexGenerationID != candidate.ID || jobRow.DocumentID != nil {
		t.Fatalf("job row = %#v", jobRow)
	}

	now := time.Now().UTC()
	if err := database.WithContext(ctx).Model(&IndexGenerationRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, candidate.ID).
		Updates(map[string]any{"status": string(value.IndexGenerationReady), "ready_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	folderID := uuid.New()
	if err := database.WithContext(ctx).Exec(
		"INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, parent_id, node_type, name) "+
			"VALUES (?, ?, ?, ?, 'folder', 'before')",
		folderID, seed.workspaceID, seed.kbID, seed.rootID,
	).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctx).Model(&FileTreeNodeRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, folderID).
		Update("name", "after").Error; err != nil {
		t.Fatal(err)
	}
	if err := store.WithinWorkspace(ctx, seed.workspaceID, func(txCtx context.Context, tx appservice.IndexGenerationTx) error {
		kb, err := tx.GetKnowledgeBaseForUpdate(txCtx, seed.kbID)
		if err != nil {
			return err
		}
		candidate, err := tx.GetIndexGeneration(txCtx, candidate.ID)
		if err != nil {
			return err
		}
		base, err := tx.GetIndexGeneration(txCtx, seed.generationID)
		if err != nil {
			return err
		}
		return tx.ActivateIndexGeneration(txCtx, kb, candidate, base)
	}); err != nil {
		t.Fatal(err)
	}
	var kbRow KnowledgeBaseRow
	if err := database.WithContext(ctx).First(&kbRow, "workspace_id = ? AND id = ?", seed.workspaceID, seed.kbID).Error; err != nil {
		t.Fatal(err)
	}
	if kbRow.ActiveIndexGenerationID == nil || *kbRow.ActiveIndexGenerationID != candidate.ID || kbRow.ContentVersion != 0 {
		t.Fatalf("knowledge base = %#v", kbRow)
	}
	var baseRow IndexGenerationRow
	if err := database.WithContext(ctx).First(&baseRow, "workspace_id = ? AND id = ?", seed.workspaceID, seed.generationID).Error; err != nil {
		t.Fatal(err)
	}
	if baseRow.Status != string(value.IndexGenerationRetired) || baseRow.RetiredAt == nil {
		t.Fatalf("base generation = %#v", baseRow)
	}
}

func TestIndexGenerationStorePersistsStaleCandidate(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	store := NewIndexGenerationStore(database)
	candidate := testIndexGenerationCandidate(t, seed, value.IndexGenerationReady)
	if err := database.WithContext(ctx).Create(indexGenerationToRow(candidate)).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctx).Model(&KnowledgeBaseRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, seed.kbID).
		Update("content_version", 1).Error; err != nil {
		t.Fatal(err)
	}
	service := appservice.NewIndexGenerationService(appservice.IndexGenerationServiceDeps{Store: store})
	_, err := service.Activate(ctx, appservice.ActivateIndexGenerationInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID, GenerationID: candidate.ID,
		ActorRole: value.RoleAdmin,
	})
	if !errors.Is(err, domainerrors.ErrGenerationStale) {
		t.Fatalf("activation error = %v", err)
	}
	var row IndexGenerationRow
	if err := database.WithContext(ctx).First(&row, "workspace_id = ? AND id = ?", seed.workspaceID, candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.Status != string(value.IndexGenerationStale) {
		t.Fatalf("candidate status = %s", row.Status)
	}
}

func TestIndexGenerationBuildStoreCompletesOrStalesAtomically(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(context.Context, *testing.T, *gorm.DB, knowledgeSchemaSeed)
		wantError  error
		wantStatus value.IndexGenerationStatus
	}{
		{name: "ready", wantStatus: value.IndexGenerationReady},
		{
			name: "stale content",
			mutate: func(ctx context.Context, t *testing.T, database *gorm.DB, seed knowledgeSchemaSeed) {
				t.Helper()
				if err := database.WithContext(ctx).Model(&KnowledgeBaseRow{}).
					Where("workspace_id = ? AND id = ?", seed.workspaceID, seed.kbID).
					Update("content_version", 1).Error; err != nil {
					t.Fatal(err)
				}
			},
			wantError: domainerrors.ErrGenerationStale, wantStatus: value.IndexGenerationStale,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, database := newAuthTestDB(t)
			seed := insertKnowledgeSchemaSeed(t, ctx, database)
			store := NewIndexGenerationStore(database)
			candidate := testIndexGenerationCandidate(t, seed, value.IndexGenerationBuilding)
			job, err := model.NewJob(model.NewJobInput{
				WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
				IndexGenerationID: candidate.ID, Type: "index_generation_build", Status: value.JobStatusPending,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := store.WithinWorkspace(ctx, seed.workspaceID, func(txCtx context.Context, tx appservice.IndexGenerationTx) error {
				return tx.CreateIndexGeneration(txCtx, candidate, job)
			}); err != nil {
				t.Fatal(err)
			}
			request := appservice.IndexGenerationBuildRequest{
				WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
				GenerationID: candidate.ID, JobID: job.ID,
			}
			loaded, err := store.Load(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.Generation.ID != candidate.ID || len(loaded.Documents) != 0 {
				t.Fatalf("loaded = %#v", loaded)
			}
			if err := store.MarkRunning(ctx, request); err != nil {
				t.Fatal(err)
			}
			if test.mutate != nil {
				test.mutate(ctx, t, database, seed)
			}
			err = store.Complete(ctx, request, nil, 0, 0)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("complete error = %v, want %v", err, test.wantError)
			}
			var row IndexGenerationRow
			if err := database.WithContext(ctx).First(&row, "workspace_id = ? AND id = ?", seed.workspaceID, candidate.ID).Error; err != nil {
				t.Fatal(err)
			}
			if value.IndexGenerationStatus(row.Status) != test.wantStatus {
				t.Fatalf("status = %s, want %s", row.Status, test.wantStatus)
			}
		})
	}
}

func TestIndexGenerationBuildStoreKeepsTransientFailureRetryable(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	store := NewIndexGenerationStore(database)
	candidate := testIndexGenerationCandidate(t, seed, value.IndexGenerationBuilding)
	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		IndexGenerationID: candidate.ID, Type: "index_generation_build", Status: value.JobStatusRunning,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithinWorkspace(ctx, seed.workspaceID, func(txCtx context.Context, tx appservice.IndexGenerationTx) error {
		return tx.CreateIndexGeneration(txCtx, candidate, job)
	}); err != nil {
		t.Fatal(err)
	}
	request := appservice.IndexGenerationBuildRequest{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		GenerationID: candidate.ID, JobID: job.ID,
	}
	if err := store.RecordFailure(ctx, request, "build_error", "temporary", false); err != nil {
		t.Fatal(err)
	}

	var generationRow IndexGenerationRow
	if err := database.WithContext(ctx).First(
		&generationRow, "workspace_id = ? AND id = ?", seed.workspaceID, candidate.ID,
	).Error; err != nil {
		t.Fatal(err)
	}
	if generationRow.Status != string(value.IndexGenerationBuilding) {
		t.Fatalf("generation status = %s, want building", generationRow.Status)
	}
	var jobRow JobRow
	if err := database.WithContext(ctx).First(
		&jobRow, "workspace_id = ? AND id = ?", seed.workspaceID, job.ID,
	).Error; err != nil {
		t.Fatal(err)
	}
	if jobRow.Status != string(value.JobStatusFailed) || jobRow.ErrorClass != "build_error" {
		t.Fatalf("job = %#v", jobRow)
	}
}

func TestIndexGenerationBuildStoreCompletesJobForReadyGeneration(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	store := NewIndexGenerationStore(database)
	candidate := testIndexGenerationCandidate(t, seed, value.IndexGenerationReady)
	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		IndexGenerationID: candidate.ID, Type: "index_generation_build", Status: value.JobStatusRunning,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctx).Create(indexGenerationToRow(candidate)).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctx).Create(jobV2ToRow(job)).Error; err != nil {
		t.Fatal(err)
	}
	request := appservice.IndexGenerationBuildRequest{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		GenerationID: candidate.ID, JobID: job.ID,
	}
	if err := store.Complete(ctx, request, nil, 0, 0); err != nil {
		t.Fatal(err)
	}

	var jobRow JobRow
	if err := database.WithContext(ctx).First(
		&jobRow, "workspace_id = ? AND id = ?", seed.workspaceID, job.ID,
	).Error; err != nil {
		t.Fatal(err)
	}
	if jobRow.Status != string(value.JobStatusCompleted) || jobRow.ErrorClass != "" || jobRow.ErrorMessage != "" {
		t.Fatalf("job = %#v", jobRow)
	}
}

func TestIndexGenerationBuildStoreDoesNotDowngradeReadyGenerationOnLateFailure(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	store := NewIndexGenerationStore(database)
	candidate := testIndexGenerationCandidate(t, seed, value.IndexGenerationReady)
	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		IndexGenerationID: candidate.ID, Type: "index_generation_build", Status: value.JobStatusCompleted,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctx).Create(indexGenerationToRow(candidate)).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctx).Create(jobV2ToRow(job)).Error; err != nil {
		t.Fatal(err)
	}
	request := appservice.IndexGenerationBuildRequest{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		GenerationID: candidate.ID, JobID: job.ID,
	}
	if err := store.RecordFailure(ctx, request, "build_error", "late failure", true); err != nil {
		t.Fatal(err)
	}

	var generationRow IndexGenerationRow
	if err := database.WithContext(ctx).First(
		&generationRow, "workspace_id = ? AND id = ?", seed.workspaceID, candidate.ID,
	).Error; err != nil {
		t.Fatal(err)
	}
	var jobRow JobRow
	if err := database.WithContext(ctx).First(
		&jobRow, "workspace_id = ? AND id = ?", seed.workspaceID, job.ID,
	).Error; err != nil {
		t.Fatal(err)
	}
	if generationRow.Status != string(value.IndexGenerationReady) || jobRow.Status != string(value.JobStatusCompleted) {
		t.Fatalf("generation=%s job=%s, want ready/completed", generationRow.Status, jobRow.Status)
	}
}

func testIndexGenerationCandidate(
	t *testing.T,
	seed knowledgeSchemaSeed,
	status value.IndexGenerationStatus,
) *model.IndexGeneration {
	t.Helper()
	baseID := seed.generationID
	generation, err := model.NewIndexGeneration(model.NewIndexGenerationInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID, BaseGenerationID: &baseID,
		EmbeddingModelID: seed.modelID, ProviderID: seed.providerID, ModelName: "text-embedding",
		EmbeddingDimension: 1024, ModelConfigHash: "model-hash", ChunkerVersion: 1,
		ChunkingConfig:  map[string]any{"chunk_size": 512, "chunk_overlap": 80},
		RetrievalConfig: map[string]any{"fts_config": "simple"}, ConfigHash: uuid.NewString(),
		SourceContentVersion: 0, IndexedContentVersion: 0, Status: status,
		ManualEditDisposition: value.ManualEditNotApplicable,
	})
	if err != nil {
		t.Fatal(err)
	}
	if status == value.IndexGenerationReady {
		now := time.Now().UTC()
		generation.ReadyAt = &now
	}
	return generation
}

// TestIndexGenerationCompleteBatchesLargeEntrySets 验证超过 publishEntryBatchSize
// 的整 generation 构建完成发布按批更新：全部 staging entries 都被发布，
// generation 转为 ready。
func TestIndexGenerationCompleteBatchesLargeEntrySets(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	store := NewIndexGenerationStore(database)
	candidate := testIndexGenerationCandidate(t, seed, value.IndexGenerationBuilding)
	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		IndexGenerationID: candidate.ID, Type: "index_generation_build", Status: value.JobStatusPending,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithinWorkspace(ctx, seed.workspaceID, func(txCtx context.Context, tx appservice.IndexGenerationTx) error {
		return tx.CreateIndexGeneration(txCtx, candidate, job)
	}); err != nil {
		t.Fatal(err)
	}

	documentID, revisionID := uuid.New(), uuid.New()
	if err := insertFileDocumentRevision(ctx, database, seed, documentID, revisionID, "build-large.md"); err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctx).Model(&DocumentRevisionRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, revisionID).
		Updates(map[string]any{"status": string(value.DocumentRevisionReady), "completed_at": time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}

	const entryCount = publishEntryBatchSize + 1
	set := &model.DocumentChunkSet{
		ID: uuid.New(), WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		DocumentID: documentID, DocumentRevisionID: revisionID,
		Strategy: value.ChunkStrategyStandard, ChunkerVersion: 1,
		ChunkingConfig: map[string]any{"chunk_size": 512, "chunk_overlap": 80},
		ConfigHash:     "build-batch-test", Status: value.ChunkSetBuilding, CreatedAt: time.Now().UTC(),
	}
	stored, err := NewChunkSetRepository(database).GetOrCreate(ctx, seed.workspaceID, set)
	if err != nil {
		t.Fatal(err)
	}
	chunks := make([]*model.Chunk, 0, entryCount)
	revisions := make([]*model.ChunkRevision, 0, entryCount)
	entries := make([]*model.RetrievalEntry, 0, entryCount)
	for i := 0; i < entryCount; i++ {
		chunkID := uuid.New()
		revision, err := model.NewChunkRevision(model.NewChunkRevisionInput{
			WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
			DocumentID: documentID, DocumentRevisionID: revisionID,
			ChunkSetID: stored.ID, ChunkID: chunkID, RevisionNo: 1,
			Content: "返回正文", EmbeddingContent: "检索正文",
			Enabled: true, Status: value.ChunkRevisionPending, EditSource: value.ChunkEditSourceSystem,
		})
		if err != nil {
			t.Fatal(err)
		}
		activeRevisionID := revision.ID
		chunks = append(chunks, &model.Chunk{
			ID: chunkID, WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
			DocumentID: documentID, DocumentRevisionID: revisionID, ChunkSetID: stored.ID,
			Sequence: i, SourceContent: "原始正文", SourceAnchor: value.SourceAnchor{SourceType: "test"},
			Metadata: map[string]any{}, ActiveRevisionID: &activeRevisionID, CreatedAt: time.Now().UTC(),
		})
		revisions = append(revisions, revision)
		entries = append(entries, &model.RetrievalEntry{
			ID: uuid.New(), WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
			IndexGenerationID: candidate.ID, DocumentID: documentID, DocumentRevisionID: revisionID,
			ChunkSetID: stored.ID, ChunkID: chunkID, ChunkRevisionID: revision.ID,
			State: value.RetrievalEntryStaging, SearchContent: "检索正文", Content: "返回正文",
			SourceAnchor: value.SourceAnchor{SourceType: "test"}, Metadata: map[string]any{},
			CreatedAt: time.Now().UTC(),
		})
	}
	if _, err := NewChunkSetRepository(database).Complete(ctx, seed.workspaceID, stored.ID, chunks, revisions); err != nil {
		t.Fatal(err)
	}
	vector := make([]float32, 1024)
	vector[0] = 1
	staged := make([]indexport.StageEntry, 0, entryCount)
	for i := range entries {
		staged = append(staged, indexport.StageEntry{Entry: entries[i], Embedding: vector})
	}
	for start := 0; start < len(staged); start += 500 {
		end := min(start+500, len(staged))
		if err := NewRetrievalRepository(database).StageBatch(ctx, seed.workspaceID, "simple", 1024, staged[start:end]); err != nil {
			t.Fatal(err)
		}
	}

	request := appservice.IndexGenerationBuildRequest{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		GenerationID: candidate.ID, JobID: job.ID,
	}
	if err := store.MarkRunning(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := store.Complete(ctx, request, entries, 1, entryCount); err != nil {
		t.Fatal(err)
	}
	var generationRow IndexGenerationRow
	if err := database.WithContext(ctx).First(&generationRow, "workspace_id = ? AND id = ?", seed.workspaceID, candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	if value.IndexGenerationStatus(generationRow.Status) != value.IndexGenerationReady {
		t.Fatalf("generation status = %s, want ready", generationRow.Status)
	}
	var publishedCount int64
	if err := database.WithContext(ctx).Model(&RetrievalEntryRow{}).
		Where("workspace_id = ? AND index_generation_id = ? AND state = ?", seed.workspaceID, candidate.ID, value.RetrievalEntryPublished).
		Count(&publishedCount).Error; err != nil {
		t.Fatal(err)
	}
	if publishedCount != entryCount {
		t.Fatalf("published entries = %d, want %d", publishedCount, entryCount)
	}
}
