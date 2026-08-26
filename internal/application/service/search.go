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
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/requestmeta"
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
	// Detail 选择响应档位（full/lean）；空值按 full 处理。投影不改排序。
	Detail value.SearchResultDetail
}

// SearchServiceDeps contains hybrid-search persistence and embedding dependencies.
type SearchServiceDeps struct {
	Repository         indexport.SearchRepository
	Resolver           EmbeddingClientResolver
	RerankResolver     RerankClientResolver
	SearchProfile      SearchProfileResolver
	SearchRuns         SearchRunStore
	SearchRunRetention time.Duration
	Logger             *slog.Logger
}

// SearchService executes active-Generation vector/FTS retrieval and RRF fusion.
type SearchService struct {
	repository         indexport.SearchRepository
	resolver           EmbeddingClientResolver
	rerankResolver     RerankClientResolver
	searchProfile      SearchProfileResolver
	searchRuns         SearchRunStore
	searchRunRetention time.Duration
	logger             *slog.Logger
}

// NewSearchService creates the hybrid-search use case.
func NewSearchService(deps SearchServiceDeps) *SearchService {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	retention := deps.SearchRunRetention
	if retention <= 0 {
		retention = defaultSearchRunRetention
	}
	return &SearchService{
		repository: deps.Repository, resolver: deps.Resolver,
		rerankResolver: deps.RerankResolver, searchProfile: deps.SearchProfile,
		searchRuns: deps.SearchRuns, searchRunRetention: retention, logger: logger,
	}
}

const defaultSearchRunRetention = 168 * time.Hour

// DefaultSearchRunRetention 是 SearchRun 默认保留期（168 小时）。
const DefaultSearchRunRetention = defaultSearchRunRetention

// Search returns evidence from the active Generation without generating an answer.
// 返回 *dto.SearchResponse；SearchRun 创建后发生的错误返回非空 response + error。
func (s *SearchService) Search(ctx context.Context, input SearchInput) (response *dto.SearchResponse, err error) {
	query := strings.TrimSpace(input.Query)
	if input.WorkspaceID == uuid.Nil || input.KnowledgeBaseID == uuid.Nil || query == "" {
		return nil, fmt.Errorf("%w: Search Workspace/KnowledgeBase/query 无效", domainerrors.ErrValidation)
	}
	if s.repository == nil || s.resolver == nil {
		return nil, fmt.Errorf("%w: Search dependencies 不能为空", domainerrors.ErrValidation)
	}
	stats := &searchRunStats{startedAt: time.Now(), queryChars: len([]rune(query))}
	queryHash := searchQueryHash(query)
	meta := requestmeta.From(ctx)
	var failurePhase searchFailurePhase = searchFailurePhaseRetrieval
	var recorder *searchRunRecorder
	var runGeneration *model.IndexGeneration
	var runOptions searchOptions
	var runRankingStage value.RankingStage
	var runResultCount int
	var runGenerationSnapshot model.SearchRunGeneration
	defer func() {
		stats.err = err
		if err != nil && recorder != nil {
			failureClass := classifySearchFailure(err, failurePhase)
			stage := runRankingStage
			if !stage.IsValid() {
				stage = value.RankingStageRRF
			}
			recorder.Finish(ctx, value.RetrievalStatusFailed, failureClass, stage, runResultCount, nil)
			summary := recorder.buildSummary(value.RetrievalStatusFailed, failureClass, stage, 0, nil, nil, value.SearchScopeSelected)
			summary.CompletedAt = ptrTime(time.Now())
			response = &dto.SearchResponse{Run: summary, Results: nil}
		}
	}()
	// RAG retrieval 根 span：gen_ai.operation.name=retrieval。traces 未启用时为 noop span。
	tracer := otel.Tracer("langhuan.rag")
	ctx, span := tracer.Start(ctx, "retrieval",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("gen_ai.operation.name", "retrieval"),
			attribute.String("gen_ai.data_source.id", input.KnowledgeBaseID.String()),
			attribute.String("knowledge_base_id", input.KnowledgeBaseID.String()),
			attribute.Int("query_chars", stats.queryChars),
		),
	)
	defer func() {
		stats.err = err
		// 注意：不把 query 原文写入 span（attribute 或 event）——OTel event 与 attribute
		// 一样随 OTLP 完整导出，无法单独采样/脱敏。与日志层 allowlist（不记录 query）
		// 保持一致，只记录 query_chars 元信息。
		resultCount := len(resultsOf(response))
		span.SetAttributes(
			attribute.Bool("rag.retrieval.empty_result", resultCount == 0 && err == nil),
			attribute.Int("result_count", resultCount),
			attribute.Int("vector_candidate_count", stats.vectorCandidateCount),
			attribute.Int("keyword_candidate_count", stats.keywordCandidateCount),
			attribute.Int("fused_candidate_count", stats.fusedCandidateCount),
			attribute.Int("grouped_candidate_count", stats.groupedCandidateCount),
			attribute.Bool("rerank_applied", stats.rerankApplied),
		)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
		s.logTerminal(ctx, stats, input, query)
	}()
	generation, err := s.activeGeneration(ctx, input.WorkspaceID, input.KnowledgeBaseID)
	if err != nil {
		failurePhase = searchFailurePhaseRetrieval
		return nil, err
	}
	runGeneration = generation
	options, err := searchOptionsFromGeneration(generation, input)
	if err != nil {
		failurePhase = searchFailurePhaseValidation
		return nil, err
	}
	runOptions = options
	// 读取 Generation 配置后创建 SearchRun recorder（此时 topK 已知）。
	recorder = newSearchRunRecorder(
		s.searchRuns, s.logger, time.Now, s.searchRunRetention,
		input.WorkspaceID, queryHash, stats.queryChars,
		options.vectorTopK, options.keywordTopK, options.finalTopK,
		value.SearchScopeSelected, meta.Transport, meta.RequestID, meta.PrincipalKind,
		nil,
	)
	runGenerationSnapshot = recorder.generationSnapshot(generation)
	// 查询阶段 Rerank 使用 Workspace Search Profile，而不是某个 KnowledgeBase Generation。
	// Rerank 与召回通道正交：vector 路被禁用时仍可生效。
	var rerankSnapshot *model.RerankSnapshot
	var rerankClient *ResolvedRerankClient
	if s.searchProfile != nil {
		rerankSnapshot, err = s.searchProfile.Resolve(ctx, input.WorkspaceID)
		if err != nil {
			return nil, err
		}
	}
	if rerankSnapshot != nil {
		stats.rerankEnabled = true
		stats.rerankModelID = rerankSnapshot.ModelID
		stats.rerankProviderID = rerankSnapshot.ProviderID
		if s.rerankResolver == nil {
			failurePhase = searchFailurePhaseRerank
			return nil, fmt.Errorf("%w: Rerank resolver 不能为空", domainerrors.ErrValidation)
		}
		failurePhase = searchFailurePhaseRerank
		rerankClient, err = s.rerankResolver.Resolve(ctx, input.WorkspaceID, rerankSnapshot.ModelID)
		if err != nil {
			return nil, err
		}
		if rerankClient == nil || rerankClient.ModelID != rerankSnapshot.ModelID ||
			rerankClient.ProviderID != rerankSnapshot.ProviderID ||
			rerankClient.ModelName != rerankSnapshot.ModelName ||
			rerankClient.ModelConfigHash != rerankSnapshot.ModelConfigHash {
			return nil, domainerrors.ErrRerankSnapshotMismatch
		}
	}
	// vector 路被禁用（vectorTopK=0）时跳过 query embedding：FTS-only 检索
	// 不应依赖 embedding 端点可用性。
	failurePhase = searchFailurePhaseEmbedding
	var queryVector []float32
	if options.vectorTopK > 0 {
		resolved, embedErr := s.resolver.Resolve(ctx, input.WorkspaceID, generation.EmbeddingModelID)
		if embedErr != nil {
			return nil, embedErr
		}
		if resolved == nil || resolved.Client == nil || resolved.ModelID != generation.EmbeddingModelID ||
			resolved.ProviderID != generation.ProviderID || resolved.ModelName != generation.ModelName ||
			resolved.Dimensions != generation.EmbeddingDimension {
			return nil, domainerrors.ErrDimensionMismatch
		}
		if resolved.ModelConfigHash != "" && generation.ModelConfigHash != "" && resolved.ModelConfigHash != generation.ModelConfigHash {
			return nil, domainerrors.ErrEmbeddingSnapshotMismatch
		}
		// query embedding 子 span：gen_ai.operation.name=embeddings。
		_, embedSpan := tracer.Start(ctx, "embeddings",
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(
				attribute.String("gen_ai.operation.name", "embeddings"),
				attribute.String("embedding.model_name", resolved.ModelName),
				attribute.Int("embedding.dimensions", resolved.Dimensions),
			),
		)
		embedded, embedErr := resolved.Client.Embed(ctx, embeddingport.EmbedInput{Texts: []string{query}})
		if embedErr != nil {
			embedSpan.RecordError(embedErr)
			embedSpan.SetStatus(codes.Error, embedErr.Error())
			embedSpan.End()
			return nil, embedErr
		}
		embedSpan.End()
		if embedded == nil || len(embedded.Vectors) != 1 ||
			len(embedded.Vectors[0]) != generation.EmbeddingDimension || !finiteChunkRevisionVector(embedded.Vectors[0]) {
			return nil, domainerrors.ErrInvalidEmbeddingResponse
		}
		queryVector = embedded.Vectors[0]
	}
	request := indexport.SearchRequest{
		KnowledgeBaseID: input.KnowledgeBaseID, GenerationID: generation.ID,
		Query: query, QueryEmbedding: queryVector, FTSConfig: options.ftsConfig,
		Dimension:  generation.EmbeddingDimension,
		VectorTopK: options.vectorTopK, KeywordTopK: options.keywordTopK,
	}
	var results []*dto.SearchResult
	failurePhase = searchFailurePhaseRetrieval
	err = s.repository.WithinWorkspace(ctx, input.WorkspaceID, func(txCtx context.Context, reader indexport.SearchReader) error {
		current, err := reader.GetActiveGeneration(txCtx, input.KnowledgeBaseID)
		if err != nil {
			return err
		}
		if current == nil || current.ID != generation.ID {
			return domainerrors.ErrGenerationStale
		}
		// topK=0 表示禁用该路召回：跳过 SQL 调用，RRF 对单路退化为直通排序。
		var vectorCandidates []indexport.SearchCandidate
		if options.vectorTopK > 0 {
			vectorCandidates, err = reader.VectorCandidates(txCtx, request)
			if err != nil {
				return err
			}
		}
		var keywordCandidates []indexport.SearchCandidate
		if options.keywordTopK > 0 {
			keywordCandidates, err = reader.KeywordCandidates(txCtx, request)
			if err != nil {
				return err
			}
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
			current := dto.SearchResultFromEvidence(item, generation.ID, candidate.Score, candidate.VectorScore, candidate.KeywordScore)
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
			// 归并后的最佳命中子块可能在任一来源行上，按排序结果重建 lean 证据。
			result.Evidence = dto.MatchedEvidenceOf(result.MatchedChildren[0])
		}
		// Rerank：在 parent grouping 之后、final truncate 之前执行一次重排。
		stats.groupedCandidateCount = len(results)
		rankingStage := value.RankingStageRRF
		if rerankSnapshot != nil && rerankClient != nil {
			rankables := buildRankablesWithContent(results, groupedSearchContent)
			candidateTopK := rerankSnapshot.CandidateTopK
			rerankStarted := time.Now()
			failurePhase = searchFailurePhaseRerank
			ranked, stage, rerankErr := applyRerank(txCtx, rerankClient, query, rankables, candidateTopK, rerankClient.MaxDocumentChars)
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
				if rerankSnapshot.FailureMode == value.RerankFailureFallback && isRerankRecoverable(rerankErr) {
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
		runRankingStage = rankingStage
		stats.rankingStage = string(rankingStage)
		stats.resultCount = len(results)
		runResultCount = len(results)
		if len(results) > options.finalTopK {
			results = results[:options.finalTopK]
		}
		dto.ProjectSearchDetail(results, value.NormalizeSearchResultDetail(input.Detail))
		return nil
	})
	if err != nil {
		return nil, err
	}
	runResultCount = len(results)
	// 状态映射：empty/degraded/available。
	status := value.RetrievalStatusAvailable
	if len(results) == 0 {
		status = value.RetrievalStatusEmpty
	} else if runRankingStage == value.RankingStageRRFFallback {
		status = value.RetrievalStatusDegraded
	}
	gens := []model.SearchRunGeneration{runGenerationSnapshot}
	recorder.Finish(ctx, status, "", runRankingStage, runResultCount, gens)
	summary := recorder.buildSummary(status, "", runRankingStage, runResultCount, []uuid.UUID{input.KnowledgeBaseID}, gens, value.SearchScopeSelected)
	summary.CompletedAt = ptrTime(time.Now())
	_ = runOptions
	_ = runGeneration
	return &dto.SearchResponse{Run: summary, Results: results}, nil
}

func resultsOf(response *dto.SearchResponse) []*dto.SearchResult {
	if response == nil {
		return nil
	}
	return response.Results
}

func ptrTime(t time.Time) *time.Time {
	return &t
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
	if vectorTopK < 0 || vectorTopK > maxCandidateTopK ||
		keywordTopK < 0 || keywordTopK > maxCandidateTopK ||
		(vectorTopK == 0 && keywordTopK == 0) {
		return searchOptions{}, fmt.Errorf(
			"%w: Search candidate topK 必须在 0..%d 之间，且两路不能同时为 0（0 表示禁用该路召回）",
			domainerrors.ErrValidation, maxCandidateTopK,
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
