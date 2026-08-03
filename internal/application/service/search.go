package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

// SearchInput is one Workspace/KB-scoped hybrid query with optional topK overrides.
type SearchInput struct {
	WorkspaceID, KnowledgeBaseID       uuid.UUID
	Query                              string
	VectorTopK, KeywordTopK, FinalTopK *int
}

// SearchServiceDeps contains hybrid-search persistence and embedding dependencies.
type SearchServiceDeps struct {
	Repository indexport.SearchRepository
	Resolver   EmbeddingClientResolver
}

// SearchService executes active-Generation vector/FTS retrieval and RRF fusion.
type SearchService struct {
	repository indexport.SearchRepository
	resolver   EmbeddingClientResolver
}

// NewSearchService creates the hybrid-search use case.
func NewSearchService(deps SearchServiceDeps) *SearchService {
	return &SearchService{repository: deps.Repository, resolver: deps.Resolver}
}

// Search returns evidence from the active Generation without generating an answer.
func (s *SearchService) Search(ctx context.Context, input SearchInput) ([]*dto.SearchResult, error) {
	query := strings.TrimSpace(input.Query)
	if input.WorkspaceID == uuid.Nil || input.KnowledgeBaseID == uuid.Nil || query == "" {
		return nil, fmt.Errorf("%w: Search Workspace/KnowledgeBase/query 无效", domainerrors.ErrValidation)
	}
	if s.repository == nil || s.resolver == nil {
		return nil, fmt.Errorf("%w: Search dependencies 不能为空", domainerrors.ErrValidation)
	}
	generation, err := s.activeGeneration(ctx, input.WorkspaceID, input.KnowledgeBaseID)
	if err != nil {
		return nil, err
	}
	options, err := searchOptionsFromGeneration(generation, input)
	if err != nil {
		return nil, err
	}
	resolved, err := s.resolver.Resolve(ctx, input.WorkspaceID, generation.EmbeddingModelID)
	if err != nil {
		return nil, err
	}
	if resolved == nil || resolved.Client == nil || resolved.ModelID != generation.EmbeddingModelID ||
		resolved.ProviderID != generation.ProviderID || resolved.ModelName != generation.ModelName ||
		resolved.Dimensions != generation.EmbeddingDimension {
		return nil, domainerrors.ErrDimensionMismatch
	}
	embedded, err := resolved.Client.Embed(ctx, embeddingport.EmbedInput{Texts: []string{query}})
	if err != nil {
		return nil, err
	}
	if embedded == nil || len(embedded.Vectors) != 1 ||
		len(embedded.Vectors[0]) != generation.EmbeddingDimension || !finiteChunkRevisionVector(embedded.Vectors[0]) {
		return nil, domainerrors.ErrInvalidEmbeddingResponse
	}
	request := indexport.SearchRequest{
		KnowledgeBaseID: input.KnowledgeBaseID, GenerationID: generation.ID,
		Query: query, QueryEmbedding: embedded.Vectors[0], FTSConfig: options.ftsConfig,
		Dimension:  generation.EmbeddingDimension,
		VectorTopK: options.vectorTopK, KeywordTopK: options.keywordTopK,
	}
	var results []*dto.SearchResult
	err = s.repository.WithinWorkspace(ctx, input.WorkspaceID, func(txCtx context.Context, reader indexport.SearchReader) error {
		current, err := reader.GetActiveGeneration(txCtx, input.KnowledgeBaseID)
		if err != nil {
			return err
		}
		if current == nil || current.ID != generation.ID {
			return domainerrors.ErrGenerationStale
		}
		vectorCandidates, err := reader.VectorCandidates(txCtx, request)
		if err != nil {
			return err
		}
		keywordCandidates, err := reader.KeywordCandidates(txCtx, request)
		if err != nil {
			return err
		}
		fused := ReciprocalRankFusion(vectorCandidates, keywordCandidates, options.rrfK)
		if len(fused) > options.finalTopK {
			fused = fused[:options.finalTopK]
		}
		entryIDs := make([]uuid.UUID, len(fused))
		for index := range fused {
			entryIDs[index] = fused[index].EntryID
		}
		evidence, err := reader.LoadEvidence(txCtx, input.KnowledgeBaseID, generation.ID, entryIDs)
		if err != nil {
			return err
		}
		byID := make(map[uuid.UUID]indexport.SearchEvidence, len(evidence))
		for _, item := range evidence {
			byID[item.EntryID] = item
		}
		if len(byID) != len(entryIDs) {
			return fmt.Errorf("%w: Search evidence 不完整", domainerrors.ErrConflict)
		}
		results = make([]*dto.SearchResult, len(fused))
		for index, candidate := range fused {
			item, ok := byID[candidate.EntryID]
			if !ok {
				return fmt.Errorf("%w: Search evidence 缺少 entry", domainerrors.ErrConflict)
			}
			results[index] = dto.SearchResultFromEvidence(
				item, candidate.Score, candidate.VectorScore, candidate.KeywordScore,
			)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

func (s *SearchService) activeGeneration(
	ctx context.Context,
	workspaceID, knowledgeBaseID uuid.UUID,
) (*model.IndexGeneration, error) {
	var generation *model.IndexGeneration
	err := s.repository.WithinWorkspace(ctx, workspaceID, func(txCtx context.Context, reader indexport.SearchReader) error {
		var err error
		generation, err = reader.GetActiveGeneration(txCtx, knowledgeBaseID)
		return err
	})
	if err != nil {
		return nil, err
	}
	if generation == nil || generation.WorkspaceID != workspaceID ||
		generation.KnowledgeBaseID != knowledgeBaseID || generation.Status != value.IndexGenerationReady {
		return nil, domainerrors.ErrGenerationNotReady
	}
	return generation, nil
}

type searchOptions struct {
	ftsConfig                          string
	vectorTopK, keywordTopK, finalTopK int
	rrfK                               int
}

func searchOptionsFromGeneration(generation *model.IndexGeneration, input SearchInput) (searchOptions, error) {
	if generation == nil {
		return searchOptions{}, domainerrors.ErrGenerationNotReady
	}
	config := generation.RetrievalConfig
	ftsConfig, ok := config["fts_config"].(string)
	if !ok || strings.TrimSpace(ftsConfig) == "" {
		return searchOptions{}, fmt.Errorf("%w: active Generation fts_config 无效", domainerrors.ErrValidation)
	}
	vectorTopK, err := searchConfigInt(config["vector_top_k"])
	if err != nil {
		return searchOptions{}, err
	}
	keywordTopK, err := searchConfigInt(config["keyword_top_k"])
	if err != nil {
		return searchOptions{}, err
	}
	finalTopK, err := searchConfigInt(config["final_top_k"])
	if err != nil {
		return searchOptions{}, err
	}
	rrfK, err := searchConfigInt(config["rrf_k"])
	if err != nil {
		return searchOptions{}, err
	}
	if input.VectorTopK != nil {
		vectorTopK = *input.VectorTopK
	}
	if input.KeywordTopK != nil {
		keywordTopK = *input.KeywordTopK
	}
	if input.FinalTopK != nil {
		finalTopK = *input.FinalTopK
	}
	if vectorTopK < minRetrievalTopK || vectorTopK > maxCandidateTopK ||
		keywordTopK < minRetrievalTopK || keywordTopK > maxCandidateTopK {
		return searchOptions{}, fmt.Errorf(
			"%w: Search candidate topK 必须在 %d..%d 之间",
			domainerrors.ErrValidation, minRetrievalTopK, maxCandidateTopK,
		)
	}
	finalTopK = max(minRetrievalTopK, min(finalTopK, maxFinalTopK))
	return searchOptions{
		ftsConfig: strings.TrimSpace(ftsConfig), vectorTopK: vectorTopK,
		keywordTopK: keywordTopK, finalTopK: finalTopK, rrfK: rrfK,
	}, nil
}

func searchConfigInt(raw any) (int, error) {
	var value int64
	switch typed := raw.(type) {
	case int:
		value = int64(typed)
	case int64:
		value = typed
	case float64:
		if math.Trunc(typed) != typed || typed > math.MaxInt64 {
			return 0, fmt.Errorf("%w: Retrieval topK/RRF 配置无效", domainerrors.ErrValidation)
		}
		value = int64(typed)
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return 0, fmt.Errorf("%w: Retrieval topK/RRF 配置无效", domainerrors.ErrValidation)
		}
		value = parsed
	default:
		return 0, fmt.Errorf("%w: Retrieval topK/RRF 配置缺失", domainerrors.ErrValidation)
	}
	if value < 1 || value > math.MaxInt {
		return 0, fmt.Errorf("%w: Retrieval topK/RRF 配置必须为正整数", domainerrors.ErrValidation)
	}
	return int(value), nil
}

// FusedSearchCandidate carries deterministic RRF rank and per-branch scores.
type FusedSearchCandidate struct {
	EntryID                   uuid.UUID
	Score                     float64
	VectorScore, KeywordScore *float64
}

// ReciprocalRankFusion combines independently ranked vector and keyword candidates.
func ReciprocalRankFusion(
	vectorCandidates, keywordCandidates []indexport.SearchCandidate,
	rrfK int,
) []FusedSearchCandidate {
	byID := make(map[uuid.UUID]*FusedSearchCandidate, len(vectorCandidates)+len(keywordCandidates))
	add := func(candidates []indexport.SearchCandidate, vector bool) {
		for index, candidate := range candidates {
			fused := byID[candidate.EntryID]
			if fused == nil {
				fused = &FusedSearchCandidate{EntryID: candidate.EntryID}
				byID[candidate.EntryID] = fused
			}
			fused.Score += 1 / float64(rrfK+index+1)
			score := candidate.Score
			if vector {
				fused.VectorScore = &score
			} else {
				fused.KeywordScore = &score
			}
		}
	}
	add(vectorCandidates, true)
	add(keywordCandidates, false)
	result := make([]FusedSearchCandidate, 0, len(byID))
	for _, candidate := range byID {
		result = append(result, *candidate)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Score != result[j].Score {
			return result[i].Score > result[j].Score
		}
		return result[i].EntryID.String() < result[j].EntryID.String()
	})
	return result
}
