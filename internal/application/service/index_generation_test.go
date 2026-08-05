package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/ports/queue"
)

func TestActivateGenerationRejectsManualEditsAndStaleContent(t *testing.T) {
	tests := []struct {
		name         string
		mutate       func(*fakeIndexGenerationStore)
		archiveEdits bool
		wantError    error
		wantPersist  bool
	}{
		{
			name: "manual edit confirmation required",
			mutate: func(store *fakeIndexGenerationStore) {
				store.candidate.ManualEditDisposition = value.ManualEditPending
			},
			wantError: domainerrors.ErrManualEditConfirmationRequired,
		},
		{
			name: "content version stale",
			mutate: func(store *fakeIndexGenerationStore) {
				store.kb.ContentVersion++
			},
			archiveEdits: true,
			wantError:    domainerrors.ErrGenerationStale,
			wantPersist:  true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeIndexGenerationStore()
			test.mutate(store)
			service := NewIndexGenerationService(IndexGenerationServiceDeps{Store: store})
			_, err := service.Activate(context.Background(), ActivateIndexGenerationInput{
				WorkspaceID: store.kb.WorkspaceID, KnowledgeBaseID: store.kb.ID,
				GenerationID: store.candidate.ID, ArchiveManualEdits: test.archiveEdits,
				ActorRole: value.RoleAdmin,
			})
			wantCalls := 0
			if test.wantPersist {
				wantCalls = 1
			}
			if !errors.Is(err, test.wantError) || store.activateCalls != wantCalls {
				t.Fatalf("error=%v activate calls=%d", err, store.activateCalls)
			}
			if test.wantPersist && store.candidate.Status != value.IndexGenerationStale {
				t.Fatalf("candidate status = %s, want stale", store.candidate.Status)
			}
		})
	}
}

func TestActivateGenerationSwitchesReadyCandidate(t *testing.T) {
	store := newFakeIndexGenerationStore()
	store.candidate.ManualEditDisposition = value.ManualEditPending
	service := NewIndexGenerationService(IndexGenerationServiceDeps{Store: store})
	got, err := service.Activate(context.Background(), ActivateIndexGenerationInput{
		WorkspaceID: store.kb.WorkspaceID, KnowledgeBaseID: store.kb.ID,
		GenerationID: store.candidate.ID, ArchiveManualEdits: true, ActorRole: value.RoleOwner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != store.candidate.ID || store.activateCalls != 1 ||
		store.candidate.ManualEditDisposition != value.ManualEditArchiveConfirmed {
		t.Fatalf("generation=%#v calls=%d", got, store.activateCalls)
	}
}

func TestCreateGenerationSnapshotsActiveStateAndQueuesBuild(t *testing.T) {
	store := newFakeIndexGenerationStore()
	store.manualEditCount, store.disabledChunkCount = 2, 1
	binder := &generationModelBinder{resolved: testGenerationResolvedModel()}
	jobQueue := &generationQueueSpy{}
	service := NewIndexGenerationService(IndexGenerationServiceDeps{Store: store, Models: binder, Queue: jobQueue})
	chunking := value.ChunkingConfig{ChunkSize: 256, ChunkOverlap: 32}

	got, err := service.Create(context.Background(), CreateIndexGenerationInput{
		WorkspaceID: store.kb.WorkspaceID, KnowledgeBaseID: store.kb.ID,
		EmbeddingModelID: binder.resolved.Model.ID, ChunkingConfig: &chunking,
		ActorRole: value.RoleAdmin,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseGenerationID == nil || *got.BaseGenerationID != store.active.ID ||
		got.SourceContentVersion != store.kb.ContentVersion || got.Status != value.IndexGenerationBuilding ||
		got.ManualEditCount != 2 || got.DisabledChunkCount != 1 ||
		got.ManualEditDisposition != value.ManualEditPending {
		t.Fatalf("generation = %#v", got)
	}
	if store.createdJob == nil || store.createdJob.IndexGenerationID != got.ID || len(jobQueue.requests) != 1 {
		t.Fatalf("job=%#v queue=%d", store.createdJob, len(jobQueue.requests))
	}
}

func TestCreateGenerationQueueFailureTerminatesBuildingGeneration(t *testing.T) {
	store := newFakeIndexGenerationStore()
	binder := &generationModelBinder{resolved: testGenerationResolvedModel()}
	queueErr := errors.New("redis unavailable")
	jobQueue := &generationQueueSpy{err: queueErr}
	service := NewIndexGenerationService(IndexGenerationServiceDeps{Store: store, Models: binder, Queue: jobQueue})

	_, err := service.Create(context.Background(), CreateIndexGenerationInput{
		WorkspaceID: store.kb.WorkspaceID, KnowledgeBaseID: store.kb.ID,
		EmbeddingModelID: binder.resolved.Model.ID, ActorRole: value.RoleAdmin,
	})
	if !errors.Is(err, queueErr) {
		t.Fatalf("Create error = %v, want queue error", err)
	}
	if store.failureCalls != 1 || !store.failureTerminal || store.failureRequest.GenerationID != store.candidate.ID {
		t.Fatalf("failure calls=%d terminal=%v request=%#v", store.failureCalls, store.failureTerminal, store.failureRequest)
	}
}

func TestGenerationRetrievalConfigRejectsOversizedCandidateTopK(t *testing.T) {
	_, err := generationRetrievalConfig(&RetrievalConfig{
		FTSConfig: "simple", VectorTopK: 1001, KeywordTopK: 30,
		FinalTopK: 10, RRFK: 60,
	}, nil)
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("generationRetrievalConfig error = %v, want ErrValidation", err)
	}
}

type fakeIndexGenerationStore struct {
	kb                 *model.KnowledgeBase
	active             *model.IndexGeneration
	candidate          *model.IndexGeneration
	manualEditCount    int64
	disabledChunkCount int64
	createdJob         *model.Job
	activateCalls      int
	failureCalls       int
	failureTerminal    bool
	failureRequest     IndexGenerationBuildRequest
}

func newFakeIndexGenerationStore() *fakeIndexGenerationStore {
	workspaceID, knowledgeBaseID, activeID, candidateID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	active := &model.IndexGeneration{
		ID: activeID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
		EmbeddingModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "embed",
		EmbeddingDimension: 1024, ModelConfigHash: "model", ChunkerVersion: 1,
		ChunkingConfig:  map[string]any{"chunk_size": 512, "chunk_overlap": 80},
		RetrievalConfig: map[string]any{"fts_config": "simple", "vector_top_k": 30, "keyword_top_k": 30, "final_top_k": 10, "rrf_k": 60},
		ConfigHash:      "active", SourceContentVersion: 4, IndexedContentVersion: 4,
		Status: value.IndexGenerationReady,
	}
	candidate := &model.IndexGeneration{
		ID: candidateID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
		BaseGenerationID: &activeID, SourceContentVersion: 4, IndexedContentVersion: 4,
		Status: value.IndexGenerationReady, ManualEditDisposition: value.ManualEditNotApplicable,
	}
	return &fakeIndexGenerationStore{
		kb: &model.KnowledgeBase{
			ID: knowledgeBaseID, WorkspaceID: workspaceID, ActiveIndexGenerationID: &activeID,
			ContentVersion: 4,
		},
		active: active, candidate: candidate,
	}
}

func (s *fakeIndexGenerationStore) WithinWorkspace(
	ctx context.Context,
	_ uuid.UUID,
	fn func(context.Context, IndexGenerationTx) error,
) error {
	return fn(ctx, s)
}

func (s *fakeIndexGenerationStore) List(context.Context, uuid.UUID, uuid.UUID) ([]*model.IndexGeneration, error) {
	return []*model.IndexGeneration{s.candidate, s.active}, nil
}

func (s *fakeIndexGenerationStore) GetKnowledgeBaseForUpdate(context.Context, uuid.UUID) (*model.KnowledgeBase, error) {
	return s.kb, nil
}

func (s *fakeIndexGenerationStore) GetIndexGeneration(_ context.Context, id uuid.UUID) (*model.IndexGeneration, error) {
	if id == s.active.ID {
		return s.active, nil
	}
	if id == s.candidate.ID {
		return s.candidate, nil
	}
	return nil, domainerrors.ErrNotFound
}

func (s *fakeIndexGenerationStore) GetActiveManualEditStats(context.Context, uuid.UUID) (int64, int64, error) {
	return s.manualEditCount, s.disabledChunkCount, nil
}

func (s *fakeIndexGenerationStore) CreateIndexGeneration(_ context.Context, generation *model.IndexGeneration, job *model.Job) error {
	s.candidate, s.createdJob = generation, job
	return nil
}

func (s *fakeIndexGenerationStore) ActivateIndexGeneration(
	_ context.Context,
	_ *model.KnowledgeBase,
	_ *model.IndexGeneration,
	_ *model.IndexGeneration,
) error {
	s.activateCalls++
	return nil
}

func (s *fakeIndexGenerationStore) RecordFailure(
	_ context.Context,
	request IndexGenerationBuildRequest,
	_, _ string,
	terminal bool,
) error {
	s.failureCalls++
	s.failureTerminal = terminal
	s.failureRequest = request
	return nil
}

type generationModelBinder struct{ resolved *model.ResolvedModel }

func (b *generationModelBinder) ResolveSelectable(context.Context, uuid.UUID, uuid.UUID) (*model.ResolvedModel, error) {
	return b.resolved, nil
}

func (b *generationModelBinder) ResolveSelectableModel(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ value.ModelType) (*model.ResolvedModel, error) {
	return b.resolved, nil
}

func testGenerationResolvedModel() *model.ResolvedModel {
	dimension := 1024
	return &model.ResolvedModel{
		Model:    &model.Model{ID: uuid.New(), ProviderID: uuid.New(), ModelName: "embed-v2", Dimensions: &dimension, Parameters: map[string]any{}},
		Provider: &model.ModelProvider{ID: uuid.New(), Provider: "openai", Config: map[string]any{}},
	}
}

type generationQueueSpy struct {
	requests []queue.JobRequest
	err      error
}

func (s *generationQueueSpy) Enqueue(_ context.Context, request queue.JobRequest) (*queue.JobHandle, error) {
	s.requests = append(s.requests, request)
	if s.err != nil {
		return nil, s.err
	}
	return &queue.JobHandle{ID: uuid.NewString()}, nil
}
