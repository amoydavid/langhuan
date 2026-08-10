package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

func replayServiceWithRun(run *model.SearchRun) (*SearchReplayService, *fakeSearchRunStore, *searchRepositoryFake) {
	store := &fakeSearchRunStore{getRun: run}
	repo := &searchRepositoryFake{generationsByGenID: map[uuid.UUID]*model.IndexGeneration{}}
	resolver := &chunkRevisionResolverStub{resolved: &ResolvedEmbeddingClient{
		Client:  &chunkRevisionEmbeddingSpy{dimension: 1024},
		ModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "embed", Dimensions: 1024,
	}}
	svc := NewSearchReplayService(SearchReplayDeps{
		Runs: store, Repository: repo, Resolver: resolver,
	})
	return svc, store, repo
}

func originalRun(query string) *model.SearchRun {
	now := time.Now()
	return &model.SearchRun{
		ID:              uuid.New(),
		WorkspaceID:     uuid.New(),
		RequestedScope:  value.SearchScopeSelected,
		QueryHash:       searchQueryHash(query),
		QueryChars:      len([]rune(query)),
		VectorTopK:      20,
		KeywordTopK:     20,
		FinalTopK:       10,
		RetrievalStatus: value.RetrievalStatusAvailable,
		RankingStage:    value.RankingStageRRF,
		CreatedAt:       now,
		ExpiresAt:       now.Add(168 * time.Hour),
	}
}

func TestSearchReplayRejectsDifferentQuery(t *testing.T) {
	run := originalRun("退款政策")
	svc, _, _ := replayServiceWithRun(run)
	_, err := svc.Replay(context.Background(), ReplaySearchInput{
		WorkspaceID: run.WorkspaceID, SearchRunID: run.ID,
		Query: "安装指南", ActorRole: value.RoleAdmin,
	})
	require.ErrorIs(t, err, domainerrors.ErrSearchQueryMismatch)
}

func TestSearchReplayRejectsBearer(t *testing.T) {
	run := originalRun("退款政策")
	svc, _, _ := replayServiceWithRun(run)
	_, err := svc.Replay(context.Background(), ReplaySearchInput{
		WorkspaceID: run.WorkspaceID, SearchRunID: run.ID,
		Query: "退款政策", IsAPIKey: true,
	})
	require.ErrorIs(t, err, domainerrors.ErrForbidden)
}

func TestSearchReplayRejectsMember(t *testing.T) {
	run := originalRun("退款政策")
	svc, _, _ := replayServiceWithRun(run)
	_, err := svc.Replay(context.Background(), ReplaySearchInput{
		WorkspaceID: run.WorkspaceID, SearchRunID: run.ID,
		Query: "退款政策", ActorRole: value.RoleMember,
	})
	require.ErrorIs(t, err, domainerrors.ErrForbidden)
}

func TestSearchReplayAllowsOwnerAndAdmin(t *testing.T) {
	run := originalRun("退款政策")
	gen := &model.IndexGeneration{
		ID: uuid.New(), WorkspaceID: run.WorkspaceID, KnowledgeBaseID: uuid.New(),
		EmbeddingModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "embed",
		EmbeddingDimension: 1024, Status: value.IndexGenerationReady, ConfigHash: "config-hash",
		ModelConfigHash: "model-hash",
		RetrievalConfig: map[string]any{"fts_config": "simple", "vector_top_k": 20, "keyword_top_k": 20, "final_top_k": 10, "rrf_k": 60},
	}
	run.Generations = []model.SearchRunGeneration{{
		KnowledgeBaseID: gen.KnowledgeBaseID, GenerationID: gen.ID,
		GenerationConfigHash: "config-hash", EmbeddingModelID: gen.EmbeddingModelID,
		ProviderID: gen.ProviderID, ModelName: gen.ModelName, ModelConfigHash: gen.ModelConfigHash,
		EmbeddingDimension: gen.EmbeddingDimension, RetrievalConfigHash: retrievalConfigHash(gen.RetrievalConfig),
	}}
	store := &fakeSearchRunStore{getRun: run}
	repo := &searchRepositoryFake{generationsByGenID: map[uuid.UUID]*model.IndexGeneration{gen.ID: gen}}
	resolver := &chunkRevisionResolverStub{resolved: &ResolvedEmbeddingClient{
		Client:  &chunkRevisionEmbeddingSpy{dimension: 1024},
		ModelID: gen.EmbeddingModelID, ProviderID: gen.ProviderID, ModelName: "embed", Dimensions: 1024, ModelConfigHash: "model-hash",
	}}
	svc := NewSearchReplayService(SearchReplayDeps{Runs: store, Repository: repo, Resolver: resolver})

	for _, role := range []value.WorkspaceRole{value.RoleOwner, value.RoleAdmin} {
		_, err := svc.Replay(context.Background(), ReplaySearchInput{
			WorkspaceID: run.WorkspaceID, SearchRunID: run.ID,
			Query: "退款政策", ActorRole: role,
		})
		require.NoError(t, err, "role %s should be allowed", role)
	}
}

func TestSearchReplayRejectsMissingGeneration(t *testing.T) {
	run := originalRun("退款政策")
	run.Generations = []model.SearchRunGeneration{{
		KnowledgeBaseID: uuid.New(), GenerationID: uuid.New(),
		GenerationConfigHash: "config-hash", EmbeddingModelID: uuid.New(),
		ProviderID: uuid.New(), ModelName: "embed", ModelConfigHash: "model-hash",
		EmbeddingDimension: 1024, RetrievalConfigHash: "retrieval-hash",
	}}
	// repo returns ErrNotFound for GetGeneration（generationsByGenID 为空）。
	svc, _, _ := replayServiceWithRun(run)
	_, err := svc.Replay(context.Background(), ReplaySearchInput{
		WorkspaceID: run.WorkspaceID, SearchRunID: run.ID,
		Query: "退款政策", ActorRole: value.RoleAdmin,
	})
	require.ErrorIs(t, err, domainerrors.ErrGenerationNotAvailable)
}

func TestSearchReplayCreatesNewSearchRunWithReplayOfID(t *testing.T) {
	run := originalRun("退款政策")
	gen := &model.IndexGeneration{
		ID: uuid.New(), WorkspaceID: run.WorkspaceID, KnowledgeBaseID: uuid.New(),
		EmbeddingModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "embed",
		EmbeddingDimension: 1024, Status: value.IndexGenerationReady, ConfigHash: "config-hash",
		ModelConfigHash: "model-hash",
		RetrievalConfig: map[string]any{"fts_config": "simple", "vector_top_k": 20, "keyword_top_k": 20, "final_top_k": 10, "rrf_k": 60},
	}
	run.Generations = []model.SearchRunGeneration{{
		KnowledgeBaseID: gen.KnowledgeBaseID, GenerationID: gen.ID,
		GenerationConfigHash: "config-hash", EmbeddingModelID: gen.EmbeddingModelID,
		ProviderID: gen.ProviderID, ModelName: gen.ModelName, ModelConfigHash: gen.ModelConfigHash,
		EmbeddingDimension: gen.EmbeddingDimension, RetrievalConfigHash: retrievalConfigHash(gen.RetrievalConfig),
	}}
	store := &fakeSearchRunStore{getRun: run}
	repo := &searchRepositoryFake{
		generationsByGenID: map[uuid.UUID]*model.IndexGeneration{gen.ID: gen},
		evidence:           map[uuid.UUID]indexport.SearchEvidence{},
	}
	resolver := &chunkRevisionResolverStub{resolved: &ResolvedEmbeddingClient{
		Client:  &chunkRevisionEmbeddingSpy{dimension: 1024},
		ModelID: gen.EmbeddingModelID, ProviderID: gen.ProviderID, ModelName: "embed", Dimensions: 1024, ModelConfigHash: "model-hash",
	}}
	svc := NewSearchReplayService(SearchReplayDeps{Runs: store, Repository: repo, Resolver: resolver})

	response, err := svc.Replay(context.Background(), ReplaySearchInput{
		WorkspaceID: run.WorkspaceID, SearchRunID: run.ID,
		Query: "退款政策", ActorRole: value.RoleAdmin,
	})
	require.NoError(t, err)
	require.NotNil(t, response)
	require.NotEqual(t, run.ID, response.Run.SearchID)
	require.NotNil(t, response.Run.ReplayOfID)
	require.Equal(t, run.ID, *response.Run.ReplayOfID)
}
