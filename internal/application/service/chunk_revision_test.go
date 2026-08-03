package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/ports/queue"
)

func TestChunkEditRejectsStaleBaseAndFAQBeforeWrite(t *testing.T) {
	tests := []struct {
		name      string
		kind      value.DocumentKind
		baseID    uuid.UUID
		wantError error
	}{
		{name: "stale base", kind: value.DocumentKindFile, baseID: uuid.New(), wantError: domainerrors.ErrRevisionConflict},
		{name: "faq immutable", kind: value.DocumentKindFAQ, wantError: domainerrors.ErrFAQChunkImmutable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeChunkRevisionStore(test.kind)
			baseID := test.baseID
			if baseID == uuid.Nil {
				baseID = *store.chunk.ActiveRevisionID
			}
			service := NewChunkRevisionService(store, &fakeChunkRevisionQueue{})
			_, err := service.Create(context.Background(), CreateChunkRevisionInput{
				WorkspaceID: store.chunk.WorkspaceID, KnowledgeBaseID: store.chunk.KnowledgeBaseID,
				ChunkID: store.chunk.ID, BaseRevisionID: baseID,
				Content: "新内容", ContextHeader: "标题", Enabled: true,
				EditorUserID: uuid.New(), ActorRole: value.RoleAdmin,
			})
			if !errors.Is(err, test.wantError) {
				t.Fatalf("error = %v, want %v", err, test.wantError)
			}
			if store.createdRevision != nil {
				t.Fatalf("created revision = %#v, want none", store.createdRevision)
			}
		})
	}
}

func TestChunkEditCreatesPendingUserRevisionAndQueuesIndex(t *testing.T) {
	store := newFakeChunkRevisionStore(value.DocumentKindWeb)
	jobQueue := &fakeChunkRevisionQueue{}
	service := NewChunkRevisionService(store, jobQueue)
	baseID := *store.chunk.ActiveRevisionID
	editorID := uuid.New()

	got, err := service.Create(context.Background(), CreateChunkRevisionInput{
		WorkspaceID: store.chunk.WorkspaceID, KnowledgeBaseID: store.chunk.KnowledgeBaseID,
		ChunkID: store.chunk.ID, BaseRevisionID: baseID,
		Content: "新内容", ContextHeader: "网页标题", Enabled: true,
		EditorUserID: editorID, ActorRole: value.RoleOwner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.RevisionNo != 2 || got.BaseRevisionID == nil || *got.BaseRevisionID != baseID ||
		got.Status != value.ChunkRevisionPending || got.EditSource != value.ChunkEditSourceUser {
		t.Fatalf("revision = %#v", got)
	}
	if store.createdRevision == nil || store.createdJob == nil || len(jobQueue.requests) != 1 {
		t.Fatalf("write/queue = revision %#v job %#v requests %d", store.createdRevision, store.createdJob, len(jobQueue.requests))
	}
	var payload map[string]any
	if err := json.Unmarshal(jobQueue.requests[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["expected_content_version"] != float64(store.kb.ContentVersion) {
		t.Fatalf("expected_content_version = %#v, want %d", payload["expected_content_version"], store.kb.ContentVersion)
	}
}

func TestChunkRevisionGetAndListExposeEditorDisplayName(t *testing.T) {
	store := newFakeChunkRevisionStore(value.DocumentKindWeb)
	nickname := "林墨"
	editorID := uuid.New()
	store.base.EditSource = value.ChunkEditSourceUser
	store.base.EditorUserID = &editorID
	store.editorNickname = &nickname
	service := NewChunkRevisionService(store, &fakeChunkRevisionQueue{})

	chunk, err := service.Get(context.Background(), store.chunk.WorkspaceID, store.chunk.KnowledgeBaseID, store.chunk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if chunk.ActiveRevision == nil || chunk.ActiveRevision.EditorDisplayName != nickname {
		t.Fatalf("chunk = %#v", chunk)
	}
	revisions, err := service.List(context.Background(), store.chunk.WorkspaceID, store.chunk.KnowledgeBaseID, store.chunk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 1 || revisions[0].EditorDisplayName != nickname {
		t.Fatalf("revisions = %#v", revisions)
	}
}

type fakeChunkRevisionStore struct {
	kb              *model.KnowledgeBase
	document        *model.Document
	chunk           *model.Chunk
	base            *model.ChunkRevision
	createdRevision *model.ChunkRevision
	createdJob      *model.Job
	nextRevisionNo  int64
	editorNickname  *string
}

func newFakeChunkRevisionStore(kind value.DocumentKind) *fakeChunkRevisionStore {
	workspaceID, knowledgeBaseID, documentID := uuid.New(), uuid.New(), uuid.New()
	chunkSetID, chunkID, baseID, generationID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	return &fakeChunkRevisionStore{
		kb:       &model.KnowledgeBase{ID: knowledgeBaseID, WorkspaceID: workspaceID, ActiveIndexGenerationID: &generationID, ContentVersion: 3},
		document: &model.Document{ID: documentID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID, Kind: kind},
		chunk: &model.Chunk{ID: chunkID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
			DocumentID: documentID, DocumentRevisionID: uuid.New(), ChunkSetID: chunkSetID, ActiveRevisionID: &baseID},
		base: &model.ChunkRevision{ID: baseID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
			DocumentID: documentID, ChunkSetID: chunkSetID, ChunkID: chunkID, RevisionNo: 1,
			Content: "旧内容", EmbeddingContent: "旧内容", Enabled: true, Status: value.ChunkRevisionReady},
		nextRevisionNo: 2,
	}
}

func (s *fakeChunkRevisionStore) WithinWorkspace(
	ctx context.Context,
	_ uuid.UUID,
	fn func(context.Context, ChunkEditTx) error,
) error {
	return fn(ctx, s)
}

func (s *fakeChunkRevisionStore) GetKnowledgeBaseForUpdate(context.Context, uuid.UUID) (*model.KnowledgeBase, error) {
	return s.kb, nil
}

func (s *fakeChunkRevisionStore) GetDocumentForUpdate(context.Context, uuid.UUID) (*model.Document, error) {
	return s.document, nil
}

func (s *fakeChunkRevisionStore) GetChunkForUpdate(context.Context, uuid.UUID) (*model.Chunk, error) {
	return s.chunk, nil
}

func (s *fakeChunkRevisionStore) GetChunkRevision(context.Context, uuid.UUID) (*model.ChunkRevision, error) {
	return s.base, nil
}

func (s *fakeChunkRevisionStore) NextChunkRevisionNo(context.Context, uuid.UUID) (int64, error) {
	next := s.nextRevisionNo
	s.nextRevisionNo++
	return next, nil
}

func (s *fakeChunkRevisionStore) CreateChunkRevisionAndJob(_ context.Context, revision *model.ChunkRevision, job *model.Job) error {
	s.createdRevision, s.createdJob = revision, job
	return nil
}

func (s *fakeChunkRevisionStore) GetChunk(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*model.Chunk, *ChunkRevisionFacts, error) {
	return s.chunk, &ChunkRevisionFacts{Revision: s.base, EditorNickname: s.editorNickname}, nil
}

func (s *fakeChunkRevisionStore) ListChunkRevisions(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) ([]*ChunkRevisionFacts, error) {
	return []*ChunkRevisionFacts{{Revision: s.base, EditorNickname: s.editorNickname}}, nil
}

type fakeChunkRevisionQueue struct{ requests []queue.JobRequest }

func (q *fakeChunkRevisionQueue) Enqueue(_ context.Context, request queue.JobRequest) (*queue.JobHandle, error) {
	q.requests = append(q.requests, request)
	return &queue.JobHandle{ID: uuid.NewString()}, nil
}
