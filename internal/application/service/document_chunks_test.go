package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestDocumentChunksIncludesDisabledActiveRevision(t *testing.T) {
	input := validDocumentChunksInput()
	facts := documentChunkFacts(input, 0, false, value.ChunkEditSourceSystem, nil)
	store := &fakeDocumentChunksStore{page: &DocumentChunkFactsPage{
		GenerationID:       uuid.New(),
		DocumentRevisionID: facts.Chunk.DocumentRevisionID,
		ChunkSetID:         facts.Chunk.ChunkSetID,
		Items:              []DocumentChunkFacts{facts},
	}}

	page, err := NewDocumentChunksService(store).List(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ActiveRevision == nil || page.Items[0].ActiveRevision.Enabled {
		t.Fatalf("page = %#v, want one disabled Chunk", page)
	}
	if page.Items[0].ActiveRevision.EditorDisplayName != "系统" {
		t.Fatalf("editor_display_name = %q, want 系统", page.Items[0].ActiveRevision.EditorDisplayName)
	}
	if store.filter.Enabled != nil {
		t.Fatalf("enabled filter = %v, want nil so disabled Chunks remain visible", *store.filter.Enabled)
	}
}

func TestDocumentChunksUsesOpaqueSequenceIDCursor(t *testing.T) {
	input := validDocumentChunksInput()
	input.Limit = 2
	first := documentChunkFacts(input, 2, true, value.ChunkEditSourceSystem, nil)
	second := documentChunkFacts(input, 3, true, value.ChunkEditSourceSystem, nil)
	third := documentChunkFacts(input, 4, true, value.ChunkEditSourceSystem, nil)
	store := &fakeDocumentChunksStore{page: &DocumentChunkFactsPage{
		GenerationID:       uuid.New(),
		DocumentRevisionID: first.Chunk.DocumentRevisionID,
		ChunkSetID:         first.Chunk.ChunkSetID,
		Items:              []DocumentChunkFacts{first, second, third},
	}}
	service := NewDocumentChunksService(store)

	page, err := service.List(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.NextCursor == nil || *page.NextCursor == "" {
		t.Fatalf("page = %#v, want two items and next cursor", page)
	}
	if store.filter.Limit != 3 {
		t.Fatalf("repository limit = %d, want requested limit + 1", store.filter.Limit)
	}

	store.page.Items = nil
	input.Cursor = *page.NextCursor
	if _, err := service.List(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if store.filter.AfterSequence == nil || *store.filter.AfterSequence != second.Chunk.Sequence ||
		store.filter.AfterID == nil || *store.filter.AfterID != second.Chunk.ID {
		t.Fatalf("decoded cursor = sequence %v id %v", store.filter.AfterSequence, store.filter.AfterID)
	}
}

func TestDocumentChunksMapsReadableEditorNames(t *testing.T) {
	input := validDocumentChunksInput()
	nickname := "林墨"
	items := []DocumentChunkFacts{
		documentChunkFacts(input, 0, true, value.ChunkEditSourceSystem, nil),
		documentChunkFacts(input, 1, true, value.ChunkEditSourceUser, &nickname),
		documentChunkFacts(input, 2, true, value.ChunkEditSourceUser, nil),
	}
	store := &fakeDocumentChunksStore{page: &DocumentChunkFactsPage{
		GenerationID: uuid.New(), DocumentRevisionID: items[0].Chunk.DocumentRevisionID,
		ChunkSetID: items[0].Chunk.ChunkSetID, Items: items,
	}}

	page, err := NewDocumentChunksService(store).List(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"系统", "林墨", "已删除用户"}
	for index, item := range page.Items {
		if item.ActiveRevision.EditorDisplayName != want[index] {
			t.Fatalf("item %d editor_display_name = %q, want %q", index, item.ActiveRevision.EditorDisplayName, want[index])
		}
	}
}

func TestDocumentChunksRejectsCrossWorkspaceLineage(t *testing.T) {
	store := &fakeDocumentChunksStore{err: domainerrors.ErrNotFound}
	_, err := NewDocumentChunksService(store).List(context.Background(), validDocumentChunksInput())
	if !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestDocumentChunksRejectsInvalidCursorAndLimit(t *testing.T) {
	service := NewDocumentChunksService(&fakeDocumentChunksStore{})
	for _, input := range []DocumentChunksInput{
		func() DocumentChunksInput {
			input := validDocumentChunksInput()
			input.Cursor = "not-a-cursor"
			return input
		}(),
		func() DocumentChunksInput { input := validDocumentChunksInput(); input.Limit = 201; return input }(),
	} {
		if _, err := service.List(context.Background(), input); !errors.Is(err, domainerrors.ErrValidation) {
			t.Fatalf("input = %#v error = %v, want validation", input, err)
		}
	}
}

type fakeDocumentChunksStore struct {
	page   *DocumentChunkFactsPage
	err    error
	filter DocumentChunkFactsFilter
}

func (s *fakeDocumentChunksStore) ListDocumentChunkFacts(
	_ context.Context,
	_, _, _ uuid.UUID,
	filter DocumentChunkFactsFilter,
) (*DocumentChunkFactsPage, error) {
	s.filter = filter
	return s.page, s.err
}

func validDocumentChunksInput() DocumentChunksInput {
	return DocumentChunksInput{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), DocumentID: uuid.New(),
	}
}

func documentChunkFacts(
	input DocumentChunksInput,
	sequence int,
	enabled bool,
	editSource value.ChunkEditSource,
	editorNickname *string,
) DocumentChunkFacts {
	documentRevisionID, chunkSetID, chunkID, revisionID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	return DocumentChunkFacts{
		Chunk: &model.Chunk{
			ID: chunkID, WorkspaceID: input.WorkspaceID, KnowledgeBaseID: input.KnowledgeBaseID,
			DocumentID: input.DocumentID, DocumentRevisionID: documentRevisionID,
			ChunkSetID: chunkSetID, Sequence: sequence,
		},
		ActiveRevision: ChunkRevisionFacts{
			Revision: &model.ChunkRevision{
				ID: revisionID, ChunkID: chunkID, Enabled: enabled, EditSource: editSource,
				Status: value.ChunkRevisionReady,
			},
			EditorNickname: editorNickname,
		},
	}
}
