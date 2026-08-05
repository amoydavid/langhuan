package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

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
	Repository     indexport.SearchRepository
	Resolver       EmbeddingClientResolver
	RerankResolver RerankClientResolver
	Logger         *slog.Logger
}

// SearchService executes active-Generation vector/FTS retrieval and RRF fusion.
type SearchService struct {
	repository     indexport.SearchRepository
	resolver       EmbeddingClientResolver
	rerankResolver RerankClientResolver
	logger         *slog.Logger
}

// NewSearchService creates the hybrid-search use case.
func NewSearchService(deps SearchServiceDeps) *SearchService {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &SearchService{repository: deps.Repository, resolver: deps.Resolver, rerankResolver: deps.RerankResolver, logger: logger}
}

// Search returns evidence from the active Generation without generating an answer.
func (s *SearchService) Search(ctx context.Context, input SearchInput) (results []*dto.SearchResult, err error) {
	query := strings.TrimSpace(input.Query)
	if input.WorkspaceID == uuid.Nil || input.KnowledgeBaseID == uuid.Nil || query == "" {
		return nil, fmt.Errorf("%w: Search Workspace/KnowledgeBase/query 无效", domainerrors.ErrValidation)
	}
	if s.repository == nil || s.resolver == nil {
		return nil, fmt.Errorf("%w: Search dependencies 不能为空", domainerrors.ErrValidation)
	}
	stats := &searchRunStats{startedAt: time.Now(), queryChars: len([]rune(query))}
	defer func() {
		stats.err = err
		s.logTerminal(ctx, stats, input, query)
	}()
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
	if resolved.ModelConfigHash != "" && generation.ModelConfigHash != "" && resolved.ModelConfigHash != generation.ModelConfigHash {
		return nil, domainerrors.ErrRerankSnapshotMismatch
	}
	// 启用 Rerank 时解析并校验快照（仅构造客户端，不发远端请求）。
	var rerankClient *ResolvedRerankClient
	if generation.Rerank != nil {
		stats.rerankEnabled = true
		if s.rerankResolver == nil {
			return nil, fmt.Errorf("%w: Rerank resolver 不能为空", domainerrors.ErrValidation)
		}
		rerankClient, err = s.rerankResolver.Resolve(ctx, input.WorkspaceID, generation.Rerank.ModelID)
		if err != nil {
			return nil, err
		}
		if rerankClient == nil || rerankClient.ModelID != generation.Rerank.ModelID ||
			rerankClient.ProviderID != generation.Rerank.ProviderID ||
			rerankClient.ModelName != generation.Rerank.ModelName ||
			rerankClient.ModelConfigHash != generation.Rerank.ModelConfigHash {
			return nil, domainerrors.ErrRerankSnapshotMismatch
		}
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
	results = nil
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
		stats.vectorCandidateCount = len(vectorCandidates)
		stats.keywordCandidateCount = len(keywordCandidates)
		fused := ReciprocalRankFusion(vectorCandidates, keywordCandidates, options.rrfK)
		stats.fusedCandidateCount = len(fused)
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
		grouped := make(map[uuid.UUID]*dto.SearchResult, len(fused))
		groupedSearchContent := make(map[uuid.UUID][]string, len(fused))
		for _, candidate := range fused {
			item, ok := byID[candidate.EntryID]
			if !ok {
				return fmt.Errorf("%w: Search evidence 缺少 entry", domainerrors.ErrConflict)
			}
			current := dto.SearchResultFromEvidence(item, candidate.Score, candidate.VectorScore, candidate.KeywordScore)
			if prior := grouped[current.ChunkID]; prior != nil {
				prior.MatchedChildren = append(prior.MatchedChildren, current.MatchedChildren[0])
				groupedSearchContent[current.ChunkID] = append(groupedSearchContent[current.ChunkID], matchedSearchContentOf(item))
				if current.Score > prior.Score {
					prior.Score, prior.VectorScore, prior.KeywordScore = current.Score, current.VectorScore, current.KeywordScore
				}
			} else {
				grouped[current.ChunkID] = current
				groupedSearchContent[current.ChunkID] = []string{matchedSearchContentOf(item)}
			}
		}
		results = make([]*dto.SearchResult, 0, len(grouped))
		for _, result := range grouped {
			results = append(results, result)
		}
		sort.Slice(results, func(i, j int) bool {
			if results[i].Score != results[j].Score {
				return results[i].Score > results[j].Score
			}
			return results[i].ChunkID.String() < results[j].ChunkID.String()
		})
		for _, result := range results {
			sort.Slice(result.MatchedChildren, func(i, j int) bool {
				if result.MatchedChildren[i].Score != result.MatchedChildren[j].Score {
					return result.MatchedChildren[i].Score > result.MatchedChildren[j].Score
				}
				return result.MatchedChildren[i].ChunkID.String() < result.MatchedChildren[j].ChunkID.String()
			})
		}
		// Rerank：在 parent grouping 之后、final truncate 之前执行一次重排。
		stats.groupedCandidateCount = len(results)
		rankingStage := value.RankingStageRRF
		if generation.Rerank != nil && rerankClient != nil {
			rankables := buildRankablesWithContent(results, groupedSearchContent)
			candidateTopK := generation.Rerank.CandidateTopK
			rerankStarted := time.Now()
			ranked, stage, rerankErr := applyRerank(txCtx, rerankClient, rankables, candidateTopK, rerankClient.MaxDocumentChars)
			rerankMS := time.Since(rerankStarted).Milliseconds()
			rerankCandidateCount := len(rankables)
			if rerankCandidateCount > candidateTopK {
				rerankCandidateCount = candidateTopK
			}
			stats.rerankCandidateCount = rerankCandidateCount
			if rerankErr != nil {
				s.logger.DebugContext(txCtx, "rerank.call.failed",
					slog.String("event", "rerank.call.failed"),
					slog.String("provider", rerankClient.ProviderKey),
					slog.String("model_id", rerankClient.ModelID.String()),
					slog.String("provider_id", rerankClient.ProviderID.String()),
					slog.Int("candidate_count", rerankCandidateCount),
					slog.Int64("duration_ms", rerankMS),
					slog.String("error_class", errorClassOf(rerankErr)),
				)
				if generation.Rerank.FailureMode == value.RerankFailureFallback && isRerankRecoverable(rerankErr) {
					rankingStage = value.RankingStageRRFFallback
					stats.rerankFallback = true
					s.logger.WarnContext(txCtx, "search.rerank_fallback",
						slog.String("event", "search.rerank_fallback"),
						slog.String("error_class", errorClassOf(rerankErr)),
					)
				} else {
					return rerankErr
				}
			} else {
				s.logger.DebugContext(txCtx, "rerank.call.completed",
					slog.String("event", "rerank.call.completed"),
					slog.String("provider", rerankClient.ProviderKey),
					slog.String("model_id", rerankClient.ModelID.String()),
					slog.String("provider_id", rerankClient.ProviderID.String()),
					slog.Int("candidate_count", rerankCandidateCount),
					slog.Int64("duration_ms", rerankMS),
				)
				results = make([]*dto.SearchResult, len(ranked))
				for i, item := range ranked {
					results[i] = item.Result
				}
				rankingStage = stage
				stats.rerankApplied = true
			}
		}
		for _, result := range results {
			result.RankingStage = rankingStage
		}
		stats.rankingStage = string(rankingStage)
		stats.resultCount = len(results)
		if len(results) > options.finalTopK {
			results = results[:options.finalTopK]
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
