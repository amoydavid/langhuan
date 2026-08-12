//go:build integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	appservice "github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

func TestPublishDocumentAtomicallySwitchesPointersAndContentVersion(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	documentID, revisionID := uuid.New(), uuid.New()
	if err := insertFileDocumentRevision(ctx, database, seed, documentID, revisionID, "publish.md"); err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctx).Model(&DocumentRevisionRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, revisionID).
		Updates(map[string]any{"status": string(value.DocumentRevisionReady), "completed_at": time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	setID, chunk, chunkRevision := createReadyRetrievalChunk(
		t, ctx, database, seed, documentID, revisionID, "原始正文", "检索正文", "返回正文",
	)
	entry := &model.RetrievalEntry{
		ID: uuid.New(), WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		IndexGenerationID: seed.generationID, DocumentID: documentID, DocumentRevisionID: revisionID,
		ChunkSetID: setID, ChunkID: chunk.ID, ChunkRevisionID: chunkRevision.ID,
		State: value.RetrievalEntryStaging, SearchContent: "检索正文", Content: "返回正文",
		SourceAnchor: chunk.SourceAnchor, Metadata: map[string]any{}, CreatedAt: time.Now().UTC(),
	}
	vector := make([]float32, 1024)
	vector[0] = 1
	if err := NewRetrievalRepository(database, nil).StageBatch(
		ctx, seed.workspaceID, "simple", 1024,
		[]indexport.StageEntry{{Entry: entry, Embedding: vector}},
	); err != nil {
		t.Fatal(err)
	}
	source, err := NewChunkSetRepository(database).GetReadyIndexSource(ctx, seed.workspaceID, setID)
	if err != nil {
		t.Fatal(err)
	}
	publisher := NewDocumentPublishDBStore(database)
	err = publisher.WithinWorkspace(ctx, seed.workspaceID, func(txCtx context.Context, tx appservice.DocumentPublishTx) error {
		document, err := tx.GetDocumentForUpdate(txCtx, documentID)
		if err != nil {
			return err
		}
		knowledgeBase, err := tx.GetKnowledgeBaseForUpdate(txCtx, seed.kbID)
		if err != nil {
			return err
		}
		document.Status = value.DocumentStatusReady
		document.ActiveRevisionID = &revisionID
		if knowledgeBase.ID != seed.kbID {
			t.Fatalf("locked knowledge base = %s", knowledgeBase.ID)
		}
		return tx.PublishDocument(
			txCtx, document, source.ChunkSet, source.Chunks, source.Revisions,
			[]*model.RetrievalEntry{entry},
		)
	})
	if err != nil {
		t.Fatal(err)
	}

	var documentRow DocumentRow
	if err := database.WithContext(ctx).First(&documentRow, "workspace_id = ? AND id = ?", seed.workspaceID, documentID).Error; err != nil {
		t.Fatal(err)
	}
	if documentRow.ActiveRevisionID == nil || *documentRow.ActiveRevisionID != revisionID ||
		documentRow.Status != string(value.DocumentStatusReady) {
		t.Fatalf("published document = %#v", documentRow)
	}
	var knowledgeBaseRow KnowledgeBaseRow
	if err := database.WithContext(ctx).First(&knowledgeBaseRow, "workspace_id = ? AND id = ?", seed.workspaceID, seed.kbID).Error; err != nil {
		t.Fatal(err)
	}
	var generationRow IndexGenerationRow
	if err := database.WithContext(ctx).First(&generationRow, "workspace_id = ? AND id = ?", seed.workspaceID, seed.generationID).Error; err != nil {
		t.Fatal(err)
	}
	if knowledgeBaseRow.ContentVersion != 1 || generationRow.IndexedContentVersion != 1 ||
		generationRow.DocumentCount != 1 || generationRow.ChunkCount != 1 || generationRow.IndexedCount != 1 {
		t.Fatalf(
			"published stats = versions kb %d generation %d, documents %d chunks %d indexed %d",
			knowledgeBaseRow.ContentVersion, generationRow.IndexedContentVersion,
			generationRow.DocumentCount, generationRow.ChunkCount, generationRow.IndexedCount,
		)
	}
	var retrievalRow RetrievalEntryRow
	if err := database.WithContext(ctx).First(&retrievalRow, "workspace_id = ? AND id = ?", seed.workspaceID, entry.ID).Error; err != nil {
		t.Fatal(err)
	}
	if retrievalRow.State != string(value.RetrievalEntryPublished) || retrievalRow.PublishedAt == nil {
		t.Fatalf("retrieval row = %#v", retrievalRow)
	}
	var revisionRow ChunkRevisionRow
	if err := database.WithContext(ctx).First(&revisionRow, "workspace_id = ? AND id = ?", seed.workspaceID, chunkRevision.ID).Error; err != nil {
		t.Fatal(err)
	}
	if revisionRow.Status != string(value.ChunkRevisionReady) || revisionRow.IndexedAt == nil {
		t.Fatalf("chunk revision = %#v", revisionRow)
	}
}

// TestPublishDocumentBatchesLargeEntrySets 验证超过 publishEntryBatchSize 的
// 单文档发布按批更新：全部 staging entries 最终都被发布，跨批累加校验不丢行。
func TestPublishDocumentBatchesLargeEntrySets(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	documentID, revisionID := uuid.New(), uuid.New()
	if err := insertFileDocumentRevision(ctx, database, seed, documentID, revisionID, "large.md"); err != nil {
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
		ConfigHash:     "publish-batch-test", Status: value.ChunkSetBuilding, CreatedAt: time.Now().UTC(),
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
			IndexGenerationID: seed.generationID, DocumentID: documentID, DocumentRevisionID: revisionID,
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
		if err := NewRetrievalRepository(database, nil).StageBatch(ctx, seed.workspaceID, "simple", 1024, staged[start:end]); err != nil {
			t.Fatal(err)
		}
	}
	source, err := NewChunkSetRepository(database).GetReadyIndexSource(ctx, seed.workspaceID, stored.ID)
	if err != nil {
		t.Fatal(err)
	}
	publisher := NewDocumentPublishDBStore(database)
	err = publisher.WithinWorkspace(ctx, seed.workspaceID, func(txCtx context.Context, tx appservice.DocumentPublishTx) error {
		document, err := tx.GetDocumentForUpdate(txCtx, documentID)
		if err != nil {
			return err
		}
		knowledgeBase, err := tx.GetKnowledgeBaseForUpdate(txCtx, seed.kbID)
		if err != nil {
			return err
		}
		document.Status = value.DocumentStatusReady
		document.ActiveRevisionID = &revisionID
		if knowledgeBase.ID != seed.kbID {
			t.Fatalf("locked knowledge base = %s", knowledgeBase.ID)
		}
		return tx.PublishDocument(txCtx, document, source.ChunkSet, source.Chunks, source.Revisions, entries)
	})
	if err != nil {
		t.Fatal(err)
	}
	var publishedCount int64
	if err := database.WithContext(ctx).Model(&RetrievalEntryRow{}).
		Where("workspace_id = ? AND state = ?", seed.workspaceID, value.RetrievalEntryPublished).
		Count(&publishedCount).Error; err != nil {
		t.Fatal(err)
	}
	if publishedCount != entryCount {
		t.Fatalf("published entries = %d, want %d", publishedCount, entryCount)
	}
}
