package db

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestDocumentV2RowRoundTripPreservesKindsAndActiveRevision(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)
	activeRevisionID := uuid.New()
	deletedAt := now.Add(time.Hour)
	tests := []struct {
		name      string
		kind      value.DocumentKind
		sourceURI string
	}{
		{name: "file", kind: value.DocumentKindFile},
		{name: "faq", kind: value.DocumentKindFAQ},
		{name: "web", kind: value.DocumentKindWeb, sourceURI: "https://example.com/guide?a=1&b=2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			document := &model.Document{
				ID: uuid.New(), WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), Kind: tt.kind,
				Title: "title", SourceType: "unit", SourceURI: tt.sourceURI,
				Status: value.DocumentStatusPending, ActiveRevisionID: &activeRevisionID,
				Metadata: nil, CreatedAt: now, UpdatedAt: now, DeletedAt: &deletedAt,
			}

			row := documentV2ToRow(document)
			got := documentV2FromRow(row)

			if got.Kind != document.Kind || got.SourceURI != document.SourceURI || got.ActiveRevisionID == nil || *got.ActiveRevisionID != activeRevisionID {
				t.Fatalf("document round trip = %#v", got)
			}
			if got.Metadata == nil || len(got.Metadata) != 0 {
				t.Fatalf("metadata = %#v, want normalized empty map", got.Metadata)
			}
			if got.DeletedAt == nil || !got.DeletedAt.Equal(deletedAt) {
				t.Fatalf("deleted_at = %v, want %v", got.DeletedAt, deletedAt)
			}
		})
	}
}

func TestDocumentRevisionV2RowRoundTripPreservesKindAndManifest(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	completedAt := now.Add(time.Minute)
	createdBy := uuid.New()
	manifest := model.ParseManifest{
		Version: model.CurrentParseManifestVersion, Parser: "text", ParserVersion: 1,
		Blocks: []model.ParsedBlock{{
			Sequence: 0, Kind: model.BlockKindParagraph, NormalizedStart: 0, NormalizedEnd: 4,
			SourceAnchor: value.SourceAnchor{SourceType: "txt"},
		}},
	}
	revision := &model.DocumentRevision{
		ID: uuid.New(), WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), DocumentID: uuid.New(),
		Kind: value.DocumentKindFile, RevisionNo: 3, Reason: value.DocumentRevisionReasonReplace,
		OriginalFilename: "guide.txt", FileType: "txt", ContentType: "text/plain",
		RawStorageKey: "raw/guide.txt", SHA256: "abc", SizeBytes: 4,
		NormalizedMarkdown: "text", ParseManifest: &manifest, ProcessingVersion: 2,
		Status: value.DocumentRevisionReady, CreatedBy: &createdBy, CreatedAt: now, CompletedAt: &completedAt,
	}

	row, err := documentRevisionToRow(revision)
	if err != nil {
		t.Fatal(err)
	}
	got, err := documentRevisionFromRow(row)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(got, revision) {
		t.Fatalf("revision round trip\n got: %#v\nwant: %#v", got, revision)
	}
}

func TestFAQRevisionV2RowRoundTripPreservesQuestionSequence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 30, 11, 0, 0, 0, time.UTC)
	revision := &model.DocumentRevision{
		ID: uuid.New(), WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), DocumentID: uuid.New(),
		Kind: value.DocumentKindFAQ, RevisionNo: 1, Reason: value.DocumentRevisionReasonIngest,
		ProcessingVersion: 1, Status: value.DocumentRevisionReady, CreatedAt: now,
	}
	faq := &model.FAQRevision{
		DocumentRevision: revision, Answer: "Use the answer.", CreatedAt: now,
		Questions: []model.FAQRevisionQuestion{
			{ID: uuid.New(), WorkspaceID: revision.WorkspaceID, KnowledgeBaseID: revision.KnowledgeBaseID, DocumentID: revision.DocumentID, DocumentRevisionID: revision.ID, Sequence: 0, Question: "How?", NormalizedQuestion: "how?", CreatedAt: now},
			{ID: uuid.New(), WorkspaceID: revision.WorkspaceID, KnowledgeBaseID: revision.KnowledgeBaseID, DocumentID: revision.DocumentID, DocumentRevisionID: revision.ID, Sequence: 1, Question: "Why?", NormalizedQuestion: "why?", CreatedAt: now},
		},
	}

	contentRow, questionRows := faqRevisionToRows(faq)
	got, err := faqRevisionFromRows(revision, contentRow, []FAQRevisionQuestionRow{questionRows[1], questionRows[0]})
	if err != nil {
		t.Fatal(err)
	}

	if got.Answer != faq.Answer || len(got.Questions) != 2 || got.Questions[0].Question != "How?" || got.Questions[1].Question != "Why?" {
		t.Fatalf("FAQ round trip = %#v", got)
	}
}

func TestFileTreeNodeV2RowRoundTripPreservesShapes(t *testing.T) {
	t.Parallel()

	parentID := uuid.New()
	documentID := uuid.New()
	tests := []*model.FileTreeNode{
		{ID: uuid.New(), WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), NodeType: value.FileTreeNodeRoot},
		{ID: uuid.New(), WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), ParentID: &parentID, NodeType: value.FileTreeNodeFolder, Name: "Folder"},
		{ID: uuid.New(), WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), ParentID: &parentID, NodeType: value.FileTreeNodeFile, Name: "guide.pdf", DocumentID: &documentID},
	}

	for _, node := range tests {
		got := fileTreeNodeFromRow(fileTreeNodeToRow(node))
		if !reflect.DeepEqual(got, node) {
			t.Fatalf("node round trip\n got: %#v\nwant: %#v", got, node)
		}
	}
}

func TestChunkAndRevisionV2RowRoundTripPreservesSourceAndEditor(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	activeRevisionID := uuid.New()
	baseRevisionID := uuid.New()
	editorUserID := uuid.New()
	one, two := 1, 2
	chunk := &model.Chunk{
		ID: uuid.New(), WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), DocumentID: uuid.New(),
		DocumentRevisionID: uuid.New(), ChunkSetID: uuid.New(), Sequence: 4,
		SourceContent: "source", ActiveRevisionID: &activeRevisionID,
		SourceAnchor: value.SourceAnchor{SourceType: "pdf", ParagraphStart: &one, ParagraphEnd: &two},
		Metadata:     nil, CreatedAt: now,
	}
	revision := &model.ChunkRevision{
		ID: activeRevisionID, WorkspaceID: chunk.WorkspaceID, KnowledgeBaseID: chunk.KnowledgeBaseID,
		DocumentID: chunk.DocumentID, DocumentRevisionID: chunk.DocumentRevisionID,
		ChunkSetID: chunk.ChunkSetID, ChunkID: chunk.ID, RevisionNo: 2, BaseRevisionID: &baseRevisionID,
		Content: "edited", ContextHeader: "Section", EmbeddingContent: "Section\n\nedited",
		Enabled: true, Status: value.ChunkRevisionReady, EditSource: value.ChunkEditSourceUser,
		EditorUserID: &editorUserID, CreatedAt: now,
	}

	chunkRow, err := chunkV2ToRow(chunk)
	if err != nil {
		t.Fatal(err)
	}
	gotChunk, err := chunkV2FromRow(chunkRow)
	if err != nil {
		t.Fatal(err)
	}
	gotRevision := chunkRevisionFromRow(chunkRevisionToRow(revision))

	wantChunk := *chunk
	wantChunk.Metadata = map[string]any{}
	if !reflect.DeepEqual(gotChunk, &wantChunk) {
		t.Fatalf("chunk round trip\n got: %#v\nwant: %#v", gotChunk, chunk)
	}
	if !reflect.DeepEqual(gotRevision, revision) {
		t.Fatalf("chunk revision round trip\n got: %#v\nwant: %#v", gotRevision, revision)
	}
}

func TestGenerationAndRetrievalEntryV2RowRoundTrip(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 30, 13, 0, 0, 0, time.UTC)
	baseGenerationID := uuid.New()
	readyAt := now.Add(time.Minute)
	dimension := 1024
	generation := &model.IndexGeneration{
		ID: uuid.New(), WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), BaseGenerationID: &baseGenerationID,
		EmbeddingModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "embed-v2", EmbeddingDimension: dimension,
		ModelConfigHash: "model-hash", ChunkerVersion: 2, ChunkingConfig: nil, RetrievalConfig: map[string]any{"rrf_k": float64(60)},
		ConfigHash: "config-hash", SourceContentVersion: 7, IndexedContentVersion: 7,
		Status: value.IndexGenerationReady, DocumentCount: 3, ChunkCount: 8, IndexedCount: 8,
		ManualEditDisposition: value.ManualEditNotApplicable, CreatedAt: now, ReadyAt: &readyAt,
	}
	entry := &model.RetrievalEntry{
		ID: uuid.New(), WorkspaceID: generation.WorkspaceID, KnowledgeBaseID: generation.KnowledgeBaseID,
		IndexGenerationID: generation.ID, DocumentID: uuid.New(), DocumentRevisionID: uuid.New(),
		ChunkSetID: uuid.New(), ChunkID: uuid.New(), ChunkRevisionID: uuid.New(),
		State: value.RetrievalEntryPublished, SearchContent: "question one\nquestion two", Content: "answer",
		SourceAnchor: value.SourceAnchor{SourceType: "faq"}, Metadata: nil,
		FTSDocument: "'question':1", Embedding: "[0.1,0.2]", Dimension: &dimension,
		CreatedAt: now, PublishedAt: &readyAt,
	}

	gotGeneration := indexGenerationFromRow(indexGenerationToRow(generation))
	entryRow, err := retrievalEntryToRow(entry)
	if err != nil {
		t.Fatal(err)
	}
	gotEntry, err := retrievalEntryFromRow(entryRow)
	if err != nil {
		t.Fatal(err)
	}

	if gotGeneration.ChunkingConfig == nil || !reflect.DeepEqual(gotGeneration.RetrievalConfig, generation.RetrievalConfig) {
		t.Fatalf("generation configs = %#v %#v", gotGeneration.ChunkingConfig, gotGeneration.RetrievalConfig)
	}
	if gotEntry.SearchContent != entry.SearchContent || gotEntry.Content != "answer" || gotEntry.Metadata == nil {
		t.Fatalf("retrieval entry round trip = %#v", gotEntry)
	}
	rowType := reflect.TypeOf(RetrievalEntryRow{})
	if _, ok := rowType.FieldByName("SearchContent"); !ok {
		t.Fatal("RetrievalEntryRow missing SearchContent")
	}
	if _, ok := rowType.FieldByName("DocumentTitle"); ok {
		t.Fatal("RetrievalEntryRow must not persist DocumentTitle")
	}
}

func TestKnowledgeBaseChunkSetAndJobV2RowRoundTrip(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 30, 14, 0, 0, 0, time.UTC)
	activeGenerationID := uuid.New()
	deletedAt := now.Add(time.Hour)
	kb := &model.KnowledgeBase{
		ID: uuid.New(), WorkspaceID: uuid.New(), Name: "kb", Description: "desc", Metadata: nil,
		ContentVersion: 11, ActiveIndexGenerationID: &activeGenerationID, FileTreeRootID: uuid.New(),
		CreatedAt: now, UpdatedAt: now, DeletedAt: &deletedAt,
	}
	chunkSet := &model.DocumentChunkSet{
		ID: uuid.New(), WorkspaceID: kb.WorkspaceID, KnowledgeBaseID: kb.ID, DocumentID: uuid.New(),
		DocumentRevisionID: uuid.New(), Strategy: value.ChunkStrategyFAQ, ChunkerVersion: 2,
		ChunkingConfig: nil, ConfigHash: "hash", Status: value.ChunkSetReady, ChunkCount: 1,
		CreatedAt: now, ReadyAt: &now,
	}
	job := &model.Job{
		ID: uuid.New(), WorkspaceID: kb.WorkspaceID, KnowledgeBaseID: kb.ID,
		IndexGenerationID: activeGenerationID, Type: "generation.build", Status: value.JobStatusRunning,
		Attempts: 2, Payload: nil, ErrorClass: "", CreatedAt: now, UpdatedAt: now,
	}

	gotKB := knowledgeBaseV2FromRow(knowledgeBaseV2ToRow(kb))
	gotChunkSet := documentChunkSetFromRow(documentChunkSetToRow(chunkSet))
	gotJob := jobV2FromRow(jobV2ToRow(job))

	if gotKB.Metadata == nil || gotKB.ContentVersion != kb.ContentVersion || gotKB.FileTreeRootID != kb.FileTreeRootID {
		t.Fatalf("knowledge base round trip = %#v", gotKB)
	}
	if gotChunkSet.ChunkingConfig == nil || gotChunkSet.Strategy != value.ChunkStrategyFAQ {
		t.Fatalf("chunk set round trip = %#v", gotChunkSet)
	}
	if gotJob.Payload == nil || gotJob.IndexGenerationID != activeGenerationID || gotJob.DocumentID != uuid.Nil {
		t.Fatalf("job round trip = %#v", gotJob)
	}
}

func TestV2CodecRejectsUnknownTypedJSONFields(t *testing.T) {
	t.Parallel()

	_, err := sourceAnchorFromJSONMap(JSONMap{"source_type": "txt", "unexpected": true})
	if err == nil {
		t.Fatal("sourceAnchorFromJSONMap() error = nil, want unknown field rejection")
	}
}
