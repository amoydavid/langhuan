package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

func TestReciprocalRankFusionRanksSharedCandidateFirstAndBreaksTiesByUUID(t *testing.T) {
	a := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	b := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	c := uuid.MustParse("00000000-0000-0000-0000-000000000003")
	vectorScoreA, vectorScoreB := 0.9, 0.8
	keywordScoreB, keywordScoreC := 0.7, 0.6
	vector := []indexport.SearchCandidate{
		{EntryID: a, Score: vectorScoreA},
		{EntryID: b, Score: vectorScoreB},
	}
	keyword := []indexport.SearchCandidate{
		{EntryID: b, Score: keywordScoreB},
		{EntryID: c, Score: keywordScoreC},
	}

	got := ReciprocalRankFusion(vector, keyword, 60)
	if len(got) != 3 || got[0].EntryID != b || got[1].EntryID != a || got[2].EntryID != c {
		t.Fatalf("ranked candidates = %#v", got)
	}
	if got[0].VectorScore == nil || *got[0].VectorScore != vectorScoreB ||
		got[0].KeywordScore == nil || *got[0].KeywordScore != keywordScoreB {
		t.Fatalf("shared candidate scores = %#v", got[0])
	}
	if got[1].VectorScore == nil || got[1].KeywordScore != nil ||
		got[2].VectorScore != nil || got[2].KeywordScore == nil {
		t.Fatalf("branch scores = %#v", got)
	}
}

func TestSearchRejectsGenerationSwitchAfterEmbedding(t *testing.T) {
	workspaceID, knowledgeBaseID := uuid.New(), uuid.New()
	modelID, providerID := uuid.New(), uuid.New()
	first := &model.IndexGeneration{
		ID: uuid.New(), WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
		EmbeddingModelID: modelID, ProviderID: providerID, ModelName: "embed",
		EmbeddingDimension: 1024, Status: value.IndexGenerationReady,
		RetrievalConfig: map[string]any{
			"fts_config": "simple", "vector_top_k": 2, "keyword_top_k": 2,
			"final_top_k": 10, "rrf_k": 60,
		},
	}
	second := *first
	second.ID = uuid.New()
	repository := &searchRepositoryFake{generations: []*model.IndexGeneration{first, &second}}
	embedder := &chunkRevisionEmbeddingSpy{dimension: 1024}
	resolver := &chunkRevisionResolverStub{resolved: &ResolvedEmbeddingClient{
		Client: embedder, ModelID: modelID, ProviderID: providerID,
		ModelName: "embed", Dimensions: 1024,
	}}
	service := NewSearchService(SearchServiceDeps{Repository: repository, Resolver: resolver})

	_, err := service.Search(context.Background(), SearchInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID, Query: "query",
	})
	if !errors.Is(err, domainerrors.ErrGenerationStale) {
		t.Fatalf("Search error = %v, want generation stale", err)
	}
}

func TestSearchUsesActiveGenerationDefaultsAndReturnsFusedEvidence(t *testing.T) {
	workspaceID, knowledgeBaseID, generationID := uuid.New(), uuid.New(), uuid.New()
	modelID, providerID := uuid.New(), uuid.New()
	a := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	b := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	c := uuid.MustParse("00000000-0000-0000-0000-000000000003")
	generation := &model.IndexGeneration{
		ID: generationID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
		EmbeddingModelID: modelID, ProviderID: providerID, ModelName: "embed",
		EmbeddingDimension: 1024, Status: value.IndexGenerationReady,
		RetrievalConfig: map[string]any{
			"fts_config": "simple", "vector_top_k": float64(2), "keyword_top_k": float64(2),
			"final_top_k": float64(100), "rrf_k": float64(60),
		},
	}
	repository := &searchRepositoryFake{
		generation: generation,
		vector:     []indexport.SearchCandidate{{EntryID: a, Score: 0.9}, {EntryID: b, Score: 0.8}},
		keyword:    []indexport.SearchCandidate{{EntryID: b, Score: 0.7}, {EntryID: c, Score: 0.6}},
		evidence: map[uuid.UUID]indexport.SearchEvidence{
			a: {EntryID: a, ChunkID: uuid.New(), ChunkRevisionID: uuid.New(), DocumentID: uuid.New(), DocumentKind: value.DocumentKindFile, Content: "A", DocumentName: "a.md", SourceAnchor: value.SourceAnchor{SourceType: "txt"}},
			b: {EntryID: b, ChunkID: uuid.New(), ChunkRevisionID: uuid.New(), DocumentID: uuid.New(), DocumentKind: value.DocumentKindFAQ, Content: "answer", DocumentName: "退款 FAQ", SourceAnchor: value.SourceAnchor{SourceType: "faq"}},
			c: {EntryID: c, ChunkID: uuid.New(), ChunkRevisionID: uuid.New(), DocumentID: uuid.New(), DocumentKind: value.DocumentKindWeb, Content: "C", DocumentName: "web", SourceAnchor: value.SourceAnchor{SourceType: "web"}},
		},
	}
	embedder := &chunkRevisionEmbeddingSpy{dimension: 1024}
	resolver := &chunkRevisionResolverStub{resolved: &ResolvedEmbeddingClient{
		Client: embedder, ModelID: modelID, ProviderID: providerID,
		ModelName: "embed", Dimensions: 1024, BatchSize: 32,
	}}
	service := NewSearchService(SearchServiceDeps{Repository: repository, Resolver: resolver})

	got, err := service.Search(context.Background(), SearchInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID, Query: "如何退款",
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.workspaceCalls != 2 || repository.lastRequest.VectorTopK != 2 ||
		repository.lastRequest.KeywordTopK != 2 || repository.loadedLimit != 3 {
		t.Fatalf("workspace calls=%d request=%#v loaded=%d", repository.workspaceCalls, repository.lastRequest, repository.loadedLimit)
	}
	if len(embedder.inputs) != 1 || len(embedder.inputs[0].Texts) != 1 || embedder.inputs[0].Texts[0] != "如何退款" {
		t.Fatalf("embedding inputs = %#v", embedder.inputs)
	}
	if len(got) != 3 || got[0].ChunkRevisionID != repository.evidence[b].ChunkRevisionID ||
		got[0].Content != "answer" || got[0].DocumentName != "退款 FAQ" || got[0].DocumentKind != value.DocumentKindFAQ {
		t.Fatalf("results = %#v", got)
	}
}

func TestSearchRejectsOversizedCandidateTopKOverride(t *testing.T) {
	oversized := 1001
	generation := &model.IndexGeneration{RetrievalConfig: map[string]any{
		"fts_config": "simple", "vector_top_k": 30, "keyword_top_k": 30,
		"final_top_k": 10, "rrf_k": 60,
	}}

	_, err := searchOptionsFromGeneration(generation, SearchInput{VectorTopK: &oversized})
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("searchOptionsFromGeneration error = %v, want ErrValidation", err)
	}
}

type searchRepositoryFake struct {
	generation     *model.IndexGeneration
	generations    []*model.IndexGeneration
	generationGets int
	vector         []indexport.SearchCandidate
	keyword        []indexport.SearchCandidate
	evidence       map[uuid.UUID]indexport.SearchEvidence
	workspaceCalls int
	lastRequest    indexport.SearchRequest
	loadedLimit    int
}

func (s *searchRepositoryFake) WithinWorkspace(
	ctx context.Context,
	_ uuid.UUID,
	fn func(context.Context, indexport.SearchReader) error,
) error {
	s.workspaceCalls++
	return fn(ctx, s)
}

func (s *searchRepositoryFake) GetActiveGeneration(context.Context, uuid.UUID) (*model.IndexGeneration, error) {
	if len(s.generations) > 0 {
		index := min(s.generationGets, len(s.generations)-1)
		s.generationGets++
		return s.generations[index], nil
	}
	return s.generation, nil
}

func (s *searchRepositoryFake) VectorCandidates(
	_ context.Context,
	request indexport.SearchRequest,
) ([]indexport.SearchCandidate, error) {
	s.lastRequest = request
	return s.vector, nil
}

func (s *searchRepositoryFake) KeywordCandidates(
	_ context.Context,
	request indexport.SearchRequest,
) ([]indexport.SearchCandidate, error) {
	s.lastRequest = request
	return s.keyword, nil
}

func (s *searchRepositoryFake) LoadEvidence(
	_ context.Context,
	_, _ uuid.UUID,
	entryIDs []uuid.UUID,
) ([]indexport.SearchEvidence, error) {
	s.loadedLimit = len(entryIDs)
	result := make([]indexport.SearchEvidence, 0, len(entryIDs))
	for _, id := range entryIDs {
		result = append(result, s.evidence[id])
	}
	return result, nil
}
