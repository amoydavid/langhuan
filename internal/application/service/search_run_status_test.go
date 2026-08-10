package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

func searchRunTestGeneration(workspaceID, knowledgeBaseID uuid.UUID) *model.IndexGeneration {
	return &model.IndexGeneration{
		ID: uuid.New(), WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
		EmbeddingModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "embed",
		EmbeddingDimension: 1024, Status: value.IndexGenerationReady,
		RetrievalConfig: map[string]any{
			"fts_config": "simple", "vector_top_k": float64(20), "keyword_top_k": float64(20),
			"final_top_k": float64(10), "rrf_k": float64(60),
		},
	}
}

func searchRunTestResolver(modelID, providerID uuid.UUID) *chunkRevisionResolverStub {
	return &chunkRevisionResolverStub{resolved: &ResolvedEmbeddingClient{
		Client: &chunkRevisionEmbeddingSpy{dimension: 1024}, ModelID: modelID,
		ProviderID: providerID, ModelName: "embed", Dimensions: 1024,
	}}
}

func TestSearchReturnsRunAndGenerationLineage(t *testing.T) {
	workspaceID, knowledgeBaseID := uuid.New(), uuid.New()
	generation := searchRunTestGeneration(workspaceID, knowledgeBaseID)
	entryID := uuid.New()
	repository := &searchRepositoryFake{
		generation: generation,
		vector:     []indexport.SearchCandidate{{EntryID: entryID, Score: 0.9}},
		evidence: map[uuid.UUID]indexport.SearchEvidence{
			entryID: {EntryID: entryID, ChunkID: uuid.New(), ChunkRevisionID: uuid.New(), DocumentID: uuid.New(), DocumentKind: value.DocumentKindFAQ, Content: "答案", DocumentName: "FAQ", SourceAnchor: value.SourceAnchor{SourceType: "faq"}, DocumentRevisionID: uuid.New()},
		},
	}
	store := &fakeSearchRunStore{}
	service := NewSearchService(SearchServiceDeps{
		Repository: repository, Resolver: searchRunTestResolver(generation.EmbeddingModelID, generation.ProviderID),
		SearchRuns: store,
	})
	response, err := service.Search(context.Background(), SearchInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID, Query: "退款",
	})
	require.NoError(t, err)
	require.NotNil(t, response)
	require.NotEqual(t, uuid.Nil, response.Run.SearchID)
	require.Len(t, response.Run.GenerationSnapshots, 1)
	require.Equal(t, generation.ID, response.Run.GenerationSnapshots[0].GenerationID)
	require.NotEmpty(t, response.Run.QueryHash)
	require.Equal(t, value.RetrievalStatusAvailable, response.Run.RetrievalStatus)
}

func TestSearchEmptyResultsMapsToEmptyStatus(t *testing.T) {
	workspaceID, knowledgeBaseID := uuid.New(), uuid.New()
	generation := searchRunTestGeneration(workspaceID, knowledgeBaseID)
	repository := &searchRepositoryFake{
		generation: generation, vector: nil, keyword: nil, evidence: map[uuid.UUID]indexport.SearchEvidence{},
	}
	store := &fakeSearchRunStore{}
	service := NewSearchService(SearchServiceDeps{
		Repository: repository, Resolver: searchRunTestResolver(generation.EmbeddingModelID, generation.ProviderID),
		SearchRuns: store,
	})
	response, err := service.Search(context.Background(), SearchInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID, Query: "不存在的内容",
	})
	require.NoError(t, err)
	require.Equal(t, value.RetrievalStatusEmpty, response.Run.RetrievalStatus)
}

func TestSearchFailedReturnsNonEmptyResponseWithError(t *testing.T) {
	workspaceID, knowledgeBaseID := uuid.New(), uuid.New()
	generation := searchRunTestGeneration(workspaceID, knowledgeBaseID)
	repository := &searchRepositoryFake{
		generation: generation,
		vector:     []indexport.SearchCandidate{{EntryID: uuid.New(), Score: 0.9}},
		evidence: map[uuid.UUID]indexport.SearchEvidence{
			generation.ID: {EntryID: generation.ID, ChunkID: uuid.New(), ChunkRevisionID: uuid.New(), DocumentID: uuid.New(), DocumentKind: value.DocumentKindFAQ, Content: "答案", DocumentName: "FAQ", SourceAnchor: value.SourceAnchor{SourceType: "faq"}},
		},
	}
	// 使用一个会让 embedding 失败的 resolver：返回 nil client。
	badResolver := &chunkRevisionResolverStub{resolved: nil}
	store := &fakeSearchRunStore{}
	service := NewSearchService(SearchServiceDeps{
		Repository: repository, Resolver: badResolver, SearchRuns: store,
	})
	response, err := service.Search(context.Background(), SearchInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID, Query: "退款",
	})
	require.Error(t, err)
	// SearchRun 创建后的错误返回非空 response + error。
	require.NotNil(t, response)
	require.Equal(t, value.RetrievalStatusFailed, response.Run.RetrievalStatus)
	require.NotEmpty(t, response.Run.FailureClass)
}

func TestSearchRunPersistenceFailureDoesNotChangeResults(t *testing.T) {
	workspaceID, knowledgeBaseID := uuid.New(), uuid.New()
	generation := searchRunTestGeneration(workspaceID, knowledgeBaseID)
	entryID := uuid.New()
	repository := &searchRepositoryFake{
		generation: generation,
		vector:     []indexport.SearchCandidate{{EntryID: entryID, Score: 0.9}},
		evidence: map[uuid.UUID]indexport.SearchEvidence{
			entryID: {EntryID: entryID, ChunkID: uuid.New(), ChunkRevisionID: uuid.New(), DocumentID: uuid.New(), DocumentKind: value.DocumentKindFAQ, Content: "答案", DocumentName: "FAQ", SourceAnchor: value.SourceAnchor{SourceType: "faq"}},
		},
	}
	store := &fakeSearchRunStore{createErr: errors.New("db unavailable")}
	service := NewSearchService(SearchServiceDeps{
		Repository: repository, Resolver: searchRunTestResolver(generation.EmbeddingModelID, generation.ProviderID),
		SearchRuns: store,
	})
	response, err := service.Search(context.Background(), SearchInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID, Query: "退款",
	})
	require.NoError(t, err)
	require.Len(t, response.Results, 1)
}

func TestSearchPreRunValidationErrorReturnsNilResponse(t *testing.T) {
	store := &fakeSearchRunStore{}
	service := NewSearchService(SearchServiceDeps{
		Repository: &searchRepositoryFake{}, Resolver: &chunkRevisionResolverStub{}, SearchRuns: store,
	})
	response, err := service.Search(context.Background(), SearchInput{
		WorkspaceID: uuid.Nil, KnowledgeBaseID: uuid.Nil, Query: "",
	})
	require.ErrorIs(t, err, domainerrors.ErrValidation)
	require.Nil(t, response)
	// 基础校验失败不应创建 SearchRun。
	require.Nil(t, store.created)
}
