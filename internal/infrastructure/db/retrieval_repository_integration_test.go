//go:build integration

package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/pipeline"
	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	indexport "github.com/dajee/langhuan/internal/ports/index"
	"gorm.io/gorm"
)

func TestStageBatchMapsMissingFTSConfigToValidationError(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	documentID, revisionID := uuid.New(), uuid.New()
	if err := insertFileDocumentRevision(ctx, database, seed, documentID, revisionID, "guide.md"); err != nil {
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

	err := NewRetrievalRepository(database, nil).StageBatch(
		ctx, seed.workspaceID, "missing_fts_config", 1024,
		[]indexport.StageEntry{{Entry: entry, Embedding: vector}},
	)

	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("StageBatch error = %v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), "全文检索配置") {
		t.Fatalf("StageBatch error = %q, want friendly FTS config message", err)
	}
	if strings.Contains(err.Error(), "SQLSTATE") || strings.Contains(err.Error(), "to_tsvector") {
		t.Fatalf("StageBatch leaked database details: %q", err)
	}
}

func TestStageAndPublishRetrievalEntryStagesDistinctSearchAndReturnContent(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	documentID, revisionID := uuid.New(), uuid.New()
	if err := insertFileDocumentRevision(ctx, database, seed, documentID, revisionID, "guide.md"); err != nil {
		t.Fatal(err)
	}
	setID, chunk, chunkRevision := createReadyRetrievalChunk(
		t, ctx, database, seed, documentID, revisionID, "原始正文", "普通检索正文", "普通返回正文",
	)
	normalEntry := &model.RetrievalEntry{
		ID: uuid.New(), WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		IndexGenerationID: seed.generationID, DocumentID: documentID, DocumentRevisionID: revisionID,
		ChunkSetID: setID, ChunkID: chunk.ID, ChunkRevisionID: chunkRevision.ID,
		State: value.RetrievalEntryStaging, SearchContent: "普通检索正文", Content: "普通返回正文",
		SourceAnchor: chunk.SourceAnchor, Metadata: map[string]any{"kind": "file"}, CreatedAt: time.Now().UTC(),
	}
	faqDocument, faq, faqJob := testFAQPersistenceAggregate(t, seed)
	faqRepository := NewFAQRepository(database)
	if err := faqRepository.WithinWorkspace(ctx, seed.workspaceID, func(txCtx context.Context, tx appservice.FAQRevisionTx) error {
		return tx.CreateFAQRevisionAggregate(txCtx, faqDocument, faq, faqJob)
	}); err != nil {
		t.Fatal(err)
	}
	faqSetID, err := pipeline.NewFAQChunkStage(faqRepository, NewChunkSetRepository(database)).
		Build(ctx, seed.workspaceID, faq.DocumentRevision.ID)
	if err != nil {
		t.Fatal(err)
	}
	faqSource, err := NewChunkSetRepository(database).GetReadyIndexSource(ctx, seed.workspaceID, faqSetID)
	if err != nil {
		t.Fatal(err)
	}
	faqChunk, faqChunkRevision := faqSource.Chunks[0], faqSource.Revisions[0]
	faqEntry := &model.RetrievalEntry{
		ID: uuid.New(), WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		IndexGenerationID: seed.generationID, DocumentID: faqDocument.ID,
		DocumentRevisionID: faq.DocumentRevision.ID, ChunkSetID: faqSetID,
		ChunkID: faqChunk.ID, ChunkRevisionID: faqChunkRevision.ID,
		State: value.RetrievalEntryStaging, SearchContent: faqChunkRevision.EmbeddingContent,
		Content: faqChunkRevision.Content, SourceAnchor: faqChunk.SourceAnchor,
		Metadata: map[string]any{"kind": "faq"}, CreatedAt: time.Now().UTC(),
	}
	vector := make([]float32, 1024)
	vector[0] = 1
	repository := NewRetrievalRepository(database, nil)
	if err := repository.StageBatch(ctx, seed.workspaceID, "simple", 1024, []indexport.StageEntry{
		{Entry: normalEntry, Embedding: vector}, {Entry: faqEntry, Embedding: vector},
	}); err != nil {
		t.Fatal(err)
	}

	var row RetrievalEntryRow
	if err := database.WithContext(ctx).First(&row, "workspace_id = ? AND id = ?", seed.workspaceID, faqEntry.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.State != string(value.RetrievalEntryStaging) || row.SearchContent != "如何退款？\n退款流程是什么？" ||
		row.Content != "请在订单页申请退款。" || row.Embedding == nil || row.Dimension == nil || *row.Dimension != 1024 ||
		row.FTSDocument == "" {
		t.Fatalf("staged row = %#v", row)
	}
	var stagedCount int64
	if err := database.WithContext(ctx).Model(&RetrievalEntryRow{}).
		Where("workspace_id = ? AND index_generation_id = ? AND state = ?", seed.workspaceID, seed.generationID, value.RetrievalEntryStaging).
		Count(&stagedCount).Error; err != nil {
		t.Fatal(err)
	}
	if stagedCount != 2 {
		t.Fatalf("staged entries = %d, want normal + FAQ", stagedCount)
	}
	var questionHit, answerHit bool
	if err := database.WithContext(ctx).Raw(
		"SELECT fts_document @@ plainto_tsquery('simple', ?) AS question_hit, "+
			"fts_document @@ plainto_tsquery('simple', ?) AS answer_hit "+
			"FROM retrieval_entries WHERE workspace_id = ? AND id = ?",
		"如何退款？", "请在订单页申请退款。", seed.workspaceID, faqEntry.ID,
	).Row().Scan(&questionHit, &answerHit); err != nil {
		t.Fatal(err)
	}
	if !questionHit || answerHit {
		t.Fatalf("FTS hits question=%v answer=%v", questionHit, answerHit)
	}
}

func createReadyRetrievalChunk(
	t *testing.T,
	ctx context.Context,
	database *gorm.DB,
	seed knowledgeSchemaSeed,
	documentID, revisionID uuid.UUID,
	sourceContent, searchContent, content string,
) (uuid.UUID, *model.Chunk, *model.ChunkRevision) {
	t.Helper()
	set := &model.DocumentChunkSet{
		ID: uuid.New(), WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		DocumentID: documentID, DocumentRevisionID: revisionID,
		Strategy: value.ChunkStrategyStandard, ChunkerVersion: 1,
		ChunkingConfig: map[string]any{"chunk_size": 512, "chunk_overlap": 80},
		ConfigHash:     "retrieval-test", Status: value.ChunkSetBuilding, CreatedAt: time.Now().UTC(),
	}
	stored, err := NewChunkSetRepository(database).GetOrCreate(ctx, seed.workspaceID, set)
	if err != nil {
		t.Fatal(err)
	}
	chunkID := uuid.New()
	revision, err := model.NewChunkRevision(model.NewChunkRevisionInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		DocumentID: documentID, DocumentRevisionID: revisionID,
		ChunkSetID: stored.ID, ChunkID: chunkID, RevisionNo: 1,
		Content: content, EmbeddingContent: searchContent,
		Enabled: true, Status: value.ChunkRevisionPending, EditSource: value.ChunkEditSourceSystem,
	})
	if err != nil {
		t.Fatal(err)
	}
	activeRevisionID := revision.ID
	chunk := &model.Chunk{
		ID: chunkID, WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		DocumentID: documentID, DocumentRevisionID: revisionID, ChunkSetID: stored.ID,
		Sequence: 0, SourceContent: sourceContent, SourceAnchor: value.SourceAnchor{SourceType: "test"},
		Metadata: map[string]any{}, ActiveRevisionID: &activeRevisionID, CreatedAt: time.Now().UTC(),
	}
	if _, err := NewChunkSetRepository(database).Complete(
		ctx, seed.workspaceID, stored.ID, []*model.Chunk{chunk}, []*model.ChunkRevision{revision},
	); err != nil {
		t.Fatal(err)
	}
	return stored.ID, chunk, revision
}
