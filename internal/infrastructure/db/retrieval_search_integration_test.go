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

	"github.com/dajee/langhuan/internal/application/pipeline"
	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

func TestKeywordCandidatesMapsMissingFTSConfigToValidationError(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	repository := NewRetrievalRepository(database)
	request := indexport.SearchRequest{
		KnowledgeBaseID: seed.kbID, GenerationID: seed.generationID,
		Query: "测试", FTSConfig: "missing_fts_config", Dimension: 1024,
		VectorTopK: 10, KeywordTopK: 10,
	}

	err := repository.WithinWorkspace(ctx, seed.workspaceID, func(
		txCtx context.Context,
		reader indexport.SearchReader,
	) error {
		_, err := reader.KeywordCandidates(txCtx, request)
		return err
	})

	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("KeywordCandidates error = %v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), "全文检索配置") {
		t.Fatalf("KeywordCandidates error = %q, want friendly FTS config message", err)
	}
	if strings.Contains(err.Error(), "SQLSTATE") || strings.Contains(err.Error(), "plainto_tsquery") {
		t.Fatalf("KeywordCandidates leaked database details: %q", err)
	}
}

func TestLoadEvidenceReturnsParentContextForChildren(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	repository := NewRetrievalRepository(database)
	documentID, revisionID := uuid.New(), uuid.New()
	if err := insertFileDocumentRevision(ctx, database, seed, documentID, revisionID, "parent-child.md"); err != nil {
		t.Fatal(err)
	}
	setID, parent, children, revisions := createParentChildRetrievalChunks(t, ctx, database, seed, documentID, revisionID)
	vector := make([]float32, 1024)
	vector[0] = 1
	entries := make([]indexport.StageEntry, 0, len(children))
	entryIDs := make([]uuid.UUID, 0, len(children))
	for index, child := range children {
		entry := &model.RetrievalEntry{
			ID: uuid.New(), WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
			IndexGenerationID: seed.generationID, DocumentID: documentID, DocumentRevisionID: revisionID,
			ChunkSetID: setID, ChunkID: child.ID, ChunkRevisionID: revisions[index].ID,
			State: value.RetrievalEntryStaging, SearchContent: revisions[index].EmbeddingContent,
			Content: revisions[index].Content, SourceAnchor: child.SourceAnchor, Metadata: child.Metadata, CreatedAt: time.Now().UTC(),
		}
		entries = append(entries, indexport.StageEntry{Entry: entry, Embedding: vector})
		entryIDs = append(entryIDs, entry.ID)
	}
	if err := repository.StageBatch(ctx, seed.workspaceID, "simple", 1024, entries); err != nil {
		t.Fatal(err)
	}
	publishSearchEntries(t, ctx, database, seed.workspaceID, entryIDs...)

	err := repository.WithinWorkspace(ctx, seed.workspaceID, func(txCtx context.Context, reader indexport.SearchReader) error {
		evidence, err := reader.LoadEvidence(txCtx, seed.kbID, seed.generationID, entryIDs)
		if err != nil {
			return err
		}
		if len(evidence) != 2 {
			t.Fatalf("evidence = %#v", evidence)
		}
		for _, item := range evidence {
			if item.ChunkID != parent.ID || item.Content != "完整父块正文" || item.MatchedRole != value.ChunkRoleChild {
				t.Fatalf("child evidence = %#v", item)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func createParentChildRetrievalChunks(t *testing.T, ctx context.Context, database *gorm.DB, seed knowledgeSchemaSeed, documentID, revisionID uuid.UUID) (uuid.UUID, *model.Chunk, []*model.Chunk, []*model.ChunkRevision) {
	t.Helper()
	set := &model.DocumentChunkSet{ID: uuid.New(), WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID, DocumentID: documentID, DocumentRevisionID: revisionID, Strategy: value.ChunkStrategyStandard, ChunkerVersion: value.StandardChunkerVersion, ChunkingConfig: map[string]any{"enable_parent_child": true}, ConfigHash: uuid.NewString(), Status: value.ChunkSetBuilding, CreatedAt: time.Now().UTC()}
	stored, err := NewChunkSetRepository(database).GetOrCreate(ctx, seed.workspaceID, set)
	if err != nil {
		t.Fatal(err)
	}
	newChunk := func(role value.ChunkRole, parentID *uuid.UUID, sequence int, content string) (*model.Chunk, *model.ChunkRevision) {
		chunkID := uuid.New()
		revision, err := model.NewChunkRevision(model.NewChunkRevisionInput{WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID, DocumentID: documentID, DocumentRevisionID: revisionID, ChunkSetID: stored.ID, ChunkID: chunkID, RevisionNo: 1, Content: content, EmbeddingContent: content, Enabled: true, Status: value.ChunkRevisionPending, EditSource: value.ChunkEditSourceSystem})
		if err != nil {
			t.Fatal(err)
		}
		activeID := revision.ID
		return &model.Chunk{ID: chunkID, WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID, DocumentID: documentID, DocumentRevisionID: revisionID, ChunkSetID: stored.ID, Role: role, ParentChunkID: parentID, Sequence: sequence, SourceContent: content, SourceAnchor: value.SourceAnchor{SourceType: "test"}, Metadata: map[string]any{}, ActiveRevisionID: &activeID, CreatedAt: time.Now().UTC()}, revision
	}
	parent, parentRevision := newChunk(value.ChunkRoleParent, nil, 0, "完整父块正文")
	childA, revisionA := newChunk(value.ChunkRoleChild, &parent.ID, 0, "命中子块 A")
	childB, revisionB := newChunk(value.ChunkRoleChild, &parent.ID, 1, "命中子块 B")
	if _, err := NewChunkSetRepository(database).Complete(ctx, seed.workspaceID, stored.ID, []*model.Chunk{parent, childA, childB}, []*model.ChunkRevision{parentRevision, revisionA, revisionB}); err != nil {
		t.Fatal(err)
	}
	return stored.ID, parent, []*model.Chunk{childA, childB}, []*model.ChunkRevision{revisionA, revisionB}
}

func TestRetrievalSearchIsWorkspaceScopedUsesFAQQuestionsAndResolvesCurrentFileName(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seedA := insertKnowledgeSchemaSeed(t, ctx, database)
	seedB := insertKnowledgeSchemaSeed(t, ctx, database)
	repository := NewRetrievalRepository(database)
	vector := make([]float32, 1024)
	vector[0] = 1

	faqDocument, faq, faqJob := testSearchFAQAggregate(t, seedA, "questionneedle", "answeronlytoken")
	faqRepository := NewFAQRepository(database)
	if err := faqRepository.WithinWorkspace(ctx, seedA.workspaceID, func(txCtx context.Context, tx appservice.FAQRevisionTx) error {
		return tx.CreateFAQRevisionAggregate(txCtx, faqDocument, faq, faqJob)
	}); err != nil {
		t.Fatal(err)
	}
	faqSetID, err := pipeline.NewFAQChunkStage(faqRepository, NewChunkSetRepository(database)).
		Build(ctx, seedA.workspaceID, faq.DocumentRevision.ID)
	if err != nil {
		t.Fatal(err)
	}
	faqSource, err := NewChunkSetRepository(database).GetReadyIndexSource(ctx, seedA.workspaceID, faqSetID)
	if err != nil {
		t.Fatal(err)
	}
	faqChunk, faqChunkRevision := faqSource.Chunks[0], faqSource.Revisions[0]
	faqEntry := &model.RetrievalEntry{
		ID: uuid.New(), WorkspaceID: seedA.workspaceID, KnowledgeBaseID: seedA.kbID,
		IndexGenerationID: seedA.generationID, DocumentID: faqDocument.ID,
		DocumentRevisionID: faq.DocumentRevision.ID, ChunkSetID: faqSetID,
		ChunkID: faqChunk.ID, ChunkRevisionID: faqChunkRevision.ID,
		State: value.RetrievalEntryStaging, SearchContent: faqChunkRevision.EmbeddingContent,
		Content: faqChunkRevision.Content, SourceAnchor: faqChunk.SourceAnchor,
		Metadata: map[string]any{"kind": "faq"}, CreatedAt: time.Now().UTC(),
	}

	fileDocumentID, fileRevisionID := uuid.New(), uuid.New()
	if err := insertFileDocumentRevision(ctx, database, seedA, fileDocumentID, fileRevisionID, "before.md"); err != nil {
		t.Fatal(err)
	}
	fileSetID, fileChunk, fileChunkRevision := createReadyRetrievalChunk(
		t, ctx, database, seedA, fileDocumentID, fileRevisionID,
		"file source", "fileneedle", "file return content",
	)
	fileEntry := &model.RetrievalEntry{
		ID: uuid.New(), WorkspaceID: seedA.workspaceID, KnowledgeBaseID: seedA.kbID,
		IndexGenerationID: seedA.generationID, DocumentID: fileDocumentID,
		DocumentRevisionID: fileRevisionID, ChunkSetID: fileSetID,
		ChunkID: fileChunk.ID, ChunkRevisionID: fileChunkRevision.ID,
		State: value.RetrievalEntryStaging, SearchContent: fileChunkRevision.EmbeddingContent,
		Content: fileChunkRevision.Content, SourceAnchor: fileChunk.SourceAnchor,
		Metadata: map[string]any{"kind": "file"}, CreatedAt: time.Now().UTC(),
	}
	if err := repository.StageBatch(ctx, seedA.workspaceID, "simple", 1024, []indexport.StageEntry{
		{Entry: faqEntry, Embedding: vector}, {Entry: fileEntry, Embedding: vector},
	}); err != nil {
		t.Fatal(err)
	}
	publishSearchEntries(t, ctx, database, seedA.workspaceID, faqEntry.ID, fileEntry.ID)

	bDocumentID, bRevisionID := uuid.New(), uuid.New()
	if err := insertFileDocumentRevision(ctx, database, seedB, bDocumentID, bRevisionID, "tenant-b.md"); err != nil {
		t.Fatal(err)
	}
	bSetID, bChunk, bChunkRevision := createReadyRetrievalChunk(
		t, ctx, database, seedB, bDocumentID, bRevisionID,
		"tenant B", "tenantbonly", "tenant B return",
	)
	bEntry := &model.RetrievalEntry{
		ID: uuid.New(), WorkspaceID: seedB.workspaceID, KnowledgeBaseID: seedB.kbID,
		IndexGenerationID: seedB.generationID, DocumentID: bDocumentID,
		DocumentRevisionID: bRevisionID, ChunkSetID: bSetID,
		ChunkID: bChunk.ID, ChunkRevisionID: bChunkRevision.ID,
		State: value.RetrievalEntryStaging, SearchContent: bChunkRevision.EmbeddingContent,
		Content: bChunkRevision.Content, SourceAnchor: bChunk.SourceAnchor,
		Metadata: map[string]any{"kind": "file"}, CreatedAt: time.Now().UTC(),
	}
	if err := repository.StageBatch(ctx, seedB.workspaceID, "simple", 1024, []indexport.StageEntry{{Entry: bEntry, Embedding: vector}}); err != nil {
		t.Fatal(err)
	}
	publishSearchEntries(t, ctx, database, seedB.workspaceID, bEntry.ID)

	if err := repository.WithinWorkspace(ctx, seedA.workspaceID, func(txCtx context.Context, reader indexport.SearchReader) error {
		generation, err := reader.GetActiveGeneration(txCtx, seedA.kbID)
		if err != nil {
			return err
		}
		if generation.ID != seedA.generationID {
			t.Fatalf("active generation = %s", generation.ID)
		}
		request := indexport.SearchRequest{
			KnowledgeBaseID: seedA.kbID, GenerationID: seedA.generationID,
			QueryEmbedding: vector, FTSConfig: "simple", Dimension: 1024,
			VectorTopK: 10, KeywordTopK: 10,
		}
		request.Query = "questionneedle"
		questionHits, err := reader.KeywordCandidates(txCtx, request)
		if err != nil {
			return err
		}
		if len(questionHits) != 1 || questionHits[0].EntryID != faqEntry.ID {
			t.Fatalf("question hits = %#v", questionHits)
		}
		request.Query = "answeronlytoken"
		answerHits, err := reader.KeywordCandidates(txCtx, request)
		if err != nil {
			return err
		}
		if len(answerHits) != 0 {
			t.Fatalf("answer-only hits = %#v", answerHits)
		}
		request.Query = "tenantbonly"
		crossTenantHits, err := reader.KeywordCandidates(txCtx, request)
		if err != nil {
			return err
		}
		if len(crossTenantHits) != 0 {
			t.Fatalf("cross-tenant hits = %#v", crossTenantHits)
		}
		vectorHits, err := reader.VectorCandidates(txCtx, request)
		if err != nil {
			return err
		}
		if len(vectorHits) != 2 {
			t.Fatalf("Workspace A vector hits = %#v", vectorHits)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&FileTreeNodeRow{}).Where(
			"workspace_id = ? AND document_id = ?", seedA.workspaceID, fileDocumentID,
		).Update("name", "after.md").Error; err != nil {
			return err
		}
		return tx.Model(&DocumentRow{}).Where(
			"workspace_id = ? AND id = ?", seedA.workspaceID, fileDocumentID,
		).Update("title", "after.md").Error
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.WithinWorkspace(ctx, seedA.workspaceID, func(txCtx context.Context, reader indexport.SearchReader) error {
		evidence, err := reader.LoadEvidence(txCtx, seedA.kbID, seedA.generationID, []uuid.UUID{fileEntry.ID})
		if err != nil {
			return err
		}
		if len(evidence) != 1 || evidence[0].DocumentName != "after.md" || evidence[0].Content != "file return content" {
			t.Fatalf("file evidence = %#v", evidence)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRetrievalSearchVectorQueryUsesDimensionHNSWIndex(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)

	tests := []struct {
		dimension int
		querySQL  string
		indexName string
	}{
		{dimension: 798, querySQL: vectorSearch798SQL, indexName: "idx_retrieval_entries_hnsw_798"},
		{dimension: 1024, querySQL: vectorSearch1024SQL, indexName: "idx_retrieval_entries_hnsw_1024"},
		{dimension: 2048, querySQL: vectorSearch2048SQL, indexName: "idx_retrieval_entries_hnsw_2048"},
		{dimension: 3584, querySQL: vectorSearch3584SQL, indexName: "idx_retrieval_entries_hnsw_3584"},
	}
	for _, test := range tests {
		t.Run(test.indexName, func(t *testing.T) {
			var indexExists bool
			if err := database.WithContext(ctx).Raw(
				"SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = ?)", test.indexName,
			).Scan(&indexExists).Error; err != nil {
				t.Fatal(err)
			}
			if !indexExists {
				t.Fatalf("HNSW 部分索引 %s 不存在", test.indexName)
			}

			vector := make([]float32, test.dimension)
			vector[0] = 1
			literal := halfVectorLiteral(vector)
			var lines []string
			if err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				if err := tx.Exec("SET LOCAL enable_seqscan = off").Error; err != nil {
					return err
				}
				if err := tx.Exec("SET LOCAL enable_sort = off").Error; err != nil {
					return err
				}
				return tx.Raw(
					"EXPLAIN (COSTS OFF) "+test.querySQL,
					literal, seed.workspaceID, seed.kbID, seed.generationID,
					test.dimension, literal, 10,
				).Scan(&lines).Error
			}); err != nil {
				t.Fatal(err)
			}
			plan := strings.Join(lines, "\n")
			if !strings.Contains(plan, test.indexName) {
				t.Fatalf("查询计划未使用 %s:\n%s", test.indexName, plan)
			}
		})
	}
}

func testSearchFAQAggregate(
	t *testing.T,
	seed knowledgeSchemaSeed,
	question, answer string,
) (*model.Document, *model.FAQRevision, *model.Job) {
	t.Helper()
	document, err := model.NewDocumentIdentity(
		seed.workspaceID, seed.kbID, value.DocumentKindFAQ, "FAQ", "api", "", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := model.NewDocumentRevision(model.NewDocumentRevisionInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID, DocumentID: document.ID,
		Kind: value.DocumentKindFAQ, DocumentKind: value.DocumentKindFAQ,
		RevisionNo: 1, Reason: value.DocumentRevisionReasonIngest,
		ProcessingVersion: 1, Status: value.DocumentRevisionReady,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	revision.CompletedAt = &now
	faq, err := model.NewFAQRevision(model.NewFAQRevisionInput{
		DocumentRevision: revision, Answer: answer, Questions: []string{question},
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		DocumentID: document.ID, DocumentRevisionID: revision.ID,
		Type: "document_index", Status: value.JobStatusPending,
	})
	if err != nil {
		t.Fatal(err)
	}
	return document, faq, job
}

// TestRetrievalSearchZhparserChineseKeywordHits 验证中文全文检索：zhparser
// 配置（迁移 000011 引入）下中文关键词能命中；对照 simple 配置（迁移前的
// 默认值）对中文整句不做词边界切分，关键词查询无法命中自己写入的条目。
func TestRetrievalSearchZhparserChineseKeywordHits(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	repository := NewRetrievalRepository(database)
	vector := make([]float32, 1024)
	vector[0] = 1
	const chineseContent = "人工智能驱动的知识管理系统"

	// zhparser 批次：内容按中文词典切词，关键词「人工智能」应命中。
	zhDocID, zhRevID := uuid.New(), uuid.New()
	if err := insertFileDocumentRevision(ctx, database, seed, zhDocID, zhRevID, "zh.md"); err != nil {
		t.Fatal(err)
	}
	zhSetID, zhChunk, zhChunkRev := createReadyRetrievalChunk(
		t, ctx, database, seed, zhDocID, zhRevID,
		chineseContent, chineseContent, "中文返回内容",
	)
	zhEntry := &model.RetrievalEntry{
		ID: uuid.New(), WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		IndexGenerationID: seed.generationID, DocumentID: zhDocID,
		DocumentRevisionID: zhRevID, ChunkSetID: zhSetID,
		ChunkID: zhChunk.ID, ChunkRevisionID: zhChunkRev.ID,
		State: value.RetrievalEntryStaging, SearchContent: zhChunkRev.EmbeddingContent,
		Content: zhChunkRev.Content, SourceAnchor: zhChunk.SourceAnchor,
		Metadata: map[string]any{"kind": "file"}, CreatedAt: time.Now().UTC(),
	}
	if err := repository.StageBatch(ctx, seed.workspaceID, "zhparser", 1024, []indexport.StageEntry{{Entry: zhEntry, Embedding: vector}}); err != nil {
		t.Fatal(err)
	}
	publishSearchEntries(t, ctx, database, seed.workspaceID, zhEntry.ID)

	// simple 批次：同一中文内容被当作整句 token，中文关键词查询应落空。
	simpleDocID, simpleRevID := uuid.New(), uuid.New()
	if err := insertFileDocumentRevision(ctx, database, seed, simpleDocID, simpleRevID, "simple.md"); err != nil {
		t.Fatal(err)
	}
	simpleSetID, simpleChunk, simpleChunkRev := createReadyRetrievalChunk(
		t, ctx, database, seed, simpleDocID, simpleRevID,
		chineseContent, chineseContent, "中文返回内容",
	)
	simpleEntry := &model.RetrievalEntry{
		ID: uuid.New(), WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		IndexGenerationID: seed.generationID, DocumentID: simpleDocID,
		DocumentRevisionID: simpleRevID, ChunkSetID: simpleSetID,
		ChunkID: simpleChunk.ID, ChunkRevisionID: simpleChunkRev.ID,
		State: value.RetrievalEntryStaging, SearchContent: simpleChunkRev.EmbeddingContent,
		Content: simpleChunkRev.Content, SourceAnchor: simpleChunk.SourceAnchor,
		Metadata: map[string]any{"kind": "file"}, CreatedAt: time.Now().UTC(),
	}
	if err := repository.StageBatch(ctx, seed.workspaceID, "simple", 1024, []indexport.StageEntry{{Entry: simpleEntry, Embedding: vector}}); err != nil {
		t.Fatal(err)
	}
	publishSearchEntries(t, ctx, database, seed.workspaceID, simpleEntry.ID)

	if err := repository.WithinWorkspace(ctx, seed.workspaceID, func(txCtx context.Context, reader indexport.SearchReader) error {
		request := indexport.SearchRequest{
			KnowledgeBaseID: seed.kbID, GenerationID: seed.generationID,
			QueryEmbedding: vector, Dimension: 1024,
			VectorTopK: 10, KeywordTopK: 10,
		}
		request.FTSConfig, request.Query = "zhparser", "人工智能"
		zhHits, err := reader.KeywordCandidates(txCtx, request)
		if err != nil {
			return err
		}
		if len(zhHits) != 1 || zhHits[0].EntryID != zhEntry.ID {
			t.Fatalf("zhparser hits = %#v, want only %s", zhHits, zhEntry.ID)
		}

		request.FTSConfig, request.Query = "simple", "人工智能"
		simpleHits, err := reader.KeywordCandidates(txCtx, request)
		if err != nil {
			return err
		}
		for _, hit := range simpleHits {
			if hit.EntryID == simpleEntry.ID {
				t.Fatalf("simple 配置下中文关键词不应命中 simple 写入的条目: %#v", simpleHits)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func publishSearchEntries(
	t *testing.T,
	ctx context.Context,
	database *gorm.DB,
	workspaceID uuid.UUID,
	entryIDs ...uuid.UUID,
) {
	t.Helper()
	now := time.Now().UTC()
	result := database.WithContext(ctx).Model(&RetrievalEntryRow{}).Where(
		"workspace_id = ? AND id IN ? AND state = ?", workspaceID, entryIDs, value.RetrievalEntryStaging,
	).Updates(map[string]any{"state": string(value.RetrievalEntryPublished), "published_at": now})
	if result.Error != nil {
		t.Fatal(result.Error)
	}
	if result.RowsAffected != int64(len(entryIDs)) {
		t.Fatalf("published rows = %d, want %d", result.RowsAffected, len(entryIDs))
	}
}
