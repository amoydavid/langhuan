package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/requestmeta"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

// ReplaySearchInput 是管理员固定快照回放的输入。
type ReplaySearchInput struct {
	WorkspaceID uuid.UUID
	SearchRunID uuid.UUID
	Query       string
	ActorRole   value.WorkspaceRole
	IsAPIKey    bool
}

// searchSnapshotOverride 是回放专用的固定快照覆盖，不在公开 API 暴露 generation_id。
type searchSnapshotOverride struct {
	generations    map[uuid.UUID]*model.IndexGeneration // KB -> Generation
	rerankSnapshot *model.RerankSnapshot
	replayOfID     *uuid.UUID
	vectorTopK     int
	keywordTopK    int
	finalTopK      int
}

// SearchReplayService 使用原 SearchRun 记录的固定快照重放检索，仅供 owner/admin 调用。
type SearchReplayService struct {
	runs               SearchRunStore
	repository         indexport.SearchRepository
	resolver           EmbeddingClientResolver
	rerankResolver     RerankClientResolver
	searchProfile      SearchProfileResolver
	logger             interface{ Log() }
	searchRuns         SearchRunStore
	searchRunRetention time.Duration
	multiLimit         int
	multiConcurrency   int
}

// SearchReplayDeps 描述 SearchReplayService 的依赖。
type SearchReplayDeps struct {
	Runs               SearchRunStore
	Repository         indexport.SearchRepository
	Resolver           EmbeddingClientResolver
	RerankResolver     RerankClientResolver
	SearchProfile      SearchProfileResolver
	Logger             replayLogger
	SearchRunRetention time.Duration
}

type replayLogger interface {
	noopLog()
}

// NewSearchReplayService 创建回放服务。
func NewSearchReplayService(deps SearchReplayDeps) *SearchReplayService {
	retention := deps.SearchRunRetention
	if retention <= 0 {
		retention = defaultSearchRunRetention
	}
	return &SearchReplayService{
		runs:               deps.Runs,
		repository:         deps.Repository,
		resolver:           deps.Resolver,
		rerankResolver:     deps.RerankResolver,
		searchProfile:      deps.SearchProfile,
		searchRuns:         deps.Runs,
		searchRunRetention: retention,
		multiLimit:         20,
		multiConcurrency:   4,
	}
}

// Replay 使用固定快照重放一次历史检索。
func (s *SearchReplayService) Replay(ctx context.Context, input ReplaySearchInput) (*dto.SearchResponse, error) {
	// 1. 权限：Bearer API Key 不可调用；只允许 owner/admin。
	if input.IsAPIKey || (input.ActorRole != value.RoleOwner && input.ActorRole != value.RoleAdmin) {
		return nil, domainerrors.ErrForbidden
	}
	// 2. 读取原 SearchRun。
	run, err := s.runs.Get(ctx, input.WorkspaceID, input.SearchRunID)
	if err != nil {
		return nil, err
	}
	// 3. 验证 query hash。
	if searchQueryHash(input.Query) != run.QueryHash {
		return nil, domainerrors.ErrSearchQueryMismatch
	}
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return nil, fmt.Errorf("%w: 回放 query 不能为空", domainerrors.ErrValidation)
	}
	// 4. 构造固定快照覆盖。
	override, err := s.buildSnapshotOverride(ctx, input.WorkspaceID, run, query)
	if err != nil {
		return nil, err
	}
	// 5. 执行固定快照搜索。
	return s.executeSnapshot(ctx, run, override, query, input)
}

func (s *SearchReplayService) buildSnapshotOverride(
	ctx context.Context,
	workspaceID uuid.UUID,
	run *model.SearchRun,
	query string,
) (*searchSnapshotOverride, error) {
	if len(run.Generations) == 0 {
		return nil, domainerrors.ErrGenerationNotAvailable
	}
	generations := make(map[uuid.UUID]*model.IndexGeneration, len(run.Generations))
	// 读取每个 KB 对应的 Generation；若 projection 已清理则返回 ErrGenerationNotAvailable。
	err := s.repository.WithinWorkspace(ctx, workspaceID, func(txCtx context.Context, reader indexport.SearchReader) error {
		for _, genSnapshot := range run.Generations {
			gen, err := reader.GetGeneration(txCtx, genSnapshot.KnowledgeBaseID, genSnapshot.GenerationID)
			if err != nil {
				return domainerrors.ErrGenerationNotAvailable
			}
			if gen == nil || gen.Status != value.IndexGenerationReady {
				return domainerrors.ErrGenerationNotAvailable
			}
			// 验证 Generation 配置 hash 与快照一致。
			if gen.ConfigHash != genSnapshot.GenerationConfigHash ||
				gen.EmbeddingModelID != genSnapshot.EmbeddingModelID ||
				gen.ProviderID != genSnapshot.ProviderID ||
				gen.ModelName != genSnapshot.ModelName ||
				gen.ModelConfigHash != genSnapshot.ModelConfigHash ||
				gen.EmbeddingDimension != genSnapshot.EmbeddingDimension ||
				retrievalConfigHash(gen.RetrievalConfig) != genSnapshot.RetrievalConfigHash {
				return domainerrors.ErrGenerationNotAvailable
			}
			generations[genSnapshot.KnowledgeBaseID] = gen
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// 从第一个 snapshot 提取 rerank（单库）或保持 nil（多库使用 Workspace profile）。
	var rerankSnapshot *model.RerankSnapshot
	if len(run.Generations) == 1 && run.Generations[0].RerankSnapshot != nil {
		rerankSnapshot = run.Generations[0].RerankSnapshot
	}
	return &searchSnapshotOverride{
		generations:    generations,
		rerankSnapshot: rerankSnapshot,
		replayOfID:     &run.ID,
		vectorTopK:     run.VectorTopK,
		keywordTopK:    run.KeywordTopK,
		finalTopK:      run.FinalTopK,
	}, nil
}

func (s *SearchReplayService) executeSnapshot(
	ctx context.Context,
	run *model.SearchRun,
	override *searchSnapshotOverride,
	query string,
	input ReplaySearchInput,
) (*dto.SearchResponse, error) {
	queryHash := searchQueryHash(query)
	queryChars := len([]rune(query))
	meta := requestmeta.From(ctx)
	// 创建回放 SearchRun recorder。
	orderedKBIDs := make([]uuid.UUID, 0, len(override.generations))
	for kbID := range override.generations {
		orderedKBIDs = append(orderedKBIDs, kbID)
	}
	sort.Slice(orderedKBIDs, func(i, j int) bool { return orderedKBIDs[i].String() < orderedKBIDs[j].String() })
	recorder := newSearchRunRecorder(
		s.searchRuns, nil, time.Now, s.searchRunRetention,
		input.WorkspaceID, queryHash, queryChars,
		run.VectorTopK, run.KeywordTopK, run.FinalTopK,
		run.RequestedScope, meta.Transport, meta.RequestID, meta.PrincipalKind,
		override.replayOfID,
	)

	// 按 embedding group 分组并 embed。
	groups := groupReplayGenerations(override.generations)
	if err := s.embedReplayGroups(ctx, input.WorkspaceID, groups, query); err != nil {
		failureClass := classifySearchFailure(err, searchFailurePhaseEmbedding)
		recorder.Finish(ctx, value.RetrievalStatusFailed, failureClass, value.RankingStageRRF, 0, nil)
		return nil, err
	}

	// 执行固定 Generation 检索（不执行 active pointer CAS）。
	perKBFused, err := s.searchReplayPerKB(ctx, input.WorkspaceID, override, groups, query)
	if err != nil {
		failureClass := classifySearchFailure(err, searchFailurePhaseRetrieval)
		recorder.Finish(ctx, value.RetrievalStatusFailed, failureClass, value.RankingStageRRF, 0, nil)
		return nil, err
	}

	merged := mergeReplayFused(perKBFused)
	results, err := s.loadReplayEvidence(ctx, input.WorkspaceID, merged, override.generations)
	if err != nil {
		recorder.Finish(ctx, value.RetrievalStatusFailed, classifySearchFailure(err, searchFailurePhaseRetrieval), value.RankingStageRRF, 0, nil)
		return nil, err
	}
	results = groupMultiSearchResults(results)

	rankingStage := value.RankingStageRRF
	// 可选 rerank：使用原 snapshot 的 rerank 配置。
	if override.rerankSnapshot != nil && s.rerankResolver != nil {
		rerankClient, err := s.rerankResolver.Resolve(ctx, input.WorkspaceID, override.rerankSnapshot.ModelID)
		if err == nil && rerankClient != nil &&
			rerankClient.ModelID == override.rerankSnapshot.ModelID &&
			rerankClient.ProviderID == override.rerankSnapshot.ProviderID &&
			rerankClient.ModelName == override.rerankSnapshot.ModelName &&
			rerankClient.ModelConfigHash == override.rerankSnapshot.ModelConfigHash {
			searchContentByChunk := make(map[uuid.UUID][]string, len(results))
			rankables := buildRankablesWithContent(results, searchContentByChunk)
			ranked, stage, rerankErr := applyRerank(ctx, rerankClient, query, rankables, override.rerankSnapshot.CandidateTopK, rerankClient.MaxDocumentChars)
			if rerankErr == nil {
				results = make([]*dto.SearchResult, len(ranked))
				for i, item := range ranked {
					results[i] = item.Result
				}
				rankingStage = stage
			} else if override.rerankSnapshot.FailureMode == value.RerankFailureFallback && isRerankRecoverable(rerankErr) {
				rankingStage = value.RankingStageRRFFallback
			}
		}
	}

	if override.finalTopK < len(results) {
		results = results[:override.finalTopK]
	}

	status := value.RetrievalStatusAvailable
	if len(results) == 0 {
		status = value.RetrievalStatusEmpty
	} else if rankingStage == value.RankingStageRRFFallback {
		status = value.RetrievalStatusDegraded
	}

	// 构造 Generation snapshot。
	gens := make([]model.SearchRunGeneration, 0, len(orderedKBIDs))
	for _, kbID := range orderedKBIDs {
		gens = append(gens, buildMultiGenerationSnapshot(input.WorkspaceID, recorder.RunID(), override.generations[kbID]))
	}
	recorder.Finish(ctx, status, "", rankingStage, len(results), gens)
	summary := recorder.buildSummary(status, "", rankingStage, len(results), orderedKBIDs, gens, run.RequestedScope)
	summary.CompletedAt = ptrTime(time.Now())
	return &dto.SearchResponse{Run: summary, Results: results}, nil
}

// groupReplayGenerations 按 embedding 五元组分组 Generation。
func groupReplayGenerations(generations map[uuid.UUID]*model.IndexGeneration) []replayEmbeddingGroup {
	byKey := make(map[embeddingGroupKey][]uuid.UUID)
	var keyOrder []embeddingGroupKey
	for kbID, gen := range generations {
		key := embeddingGroupKey{
			EmbeddingModelID:   gen.EmbeddingModelID,
			ProviderID:         gen.ProviderID,
			ModelName:          gen.ModelName,
			EmbeddingDimension: gen.EmbeddingDimension,
			ModelConfigHash:    gen.ModelConfigHash,
		}
		if _, ok := byKey[key]; !ok {
			keyOrder = append(keyOrder, key)
		}
		byKey[key] = append(byKey[key], kbID)
	}
	sort.Slice(keyOrder, func(i, j int) bool {
		return keyOrder[i].EmbeddingModelID.String() < keyOrder[j].EmbeddingModelID.String()
	})
	groups := make([]replayEmbeddingGroup, 0, len(keyOrder))
	for _, key := range keyOrder {
		groups = append(groups, replayEmbeddingGroup{key: key, kbIDs: byKey[key]})
	}
	return groups
}

type replayEmbeddingGroup struct {
	key         embeddingGroupKey
	kbIDs       []uuid.UUID
	queryVector []float32
}

func (s *SearchReplayService) embedReplayGroups(ctx context.Context, workspaceID uuid.UUID, groups []replayEmbeddingGroup, query string) error {
	for i := range groups {
		group := &groups[i]
		resolved, err := s.resolver.Resolve(ctx, workspaceID, group.key.EmbeddingModelID)
		if err != nil {
			return err
		}
		if resolved == nil || resolved.Client == nil ||
			resolved.ModelID != group.key.EmbeddingModelID ||
			resolved.ProviderID != group.key.ProviderID ||
			resolved.ModelName != group.key.ModelName ||
			resolved.Dimensions != group.key.EmbeddingDimension {
			return domainerrors.ErrDimensionMismatch
		}
		if resolved.ModelConfigHash != "" && group.key.ModelConfigHash != "" && resolved.ModelConfigHash != group.key.ModelConfigHash {
			return domainerrors.ErrEmbeddingSnapshotMismatch
		}
		embedded, err := resolved.Client.Embed(ctx, embeddingport.EmbedInput{Texts: []string{query}})
		if err != nil {
			return err
		}
		if len(embedded.Vectors) != 1 || len(embedded.Vectors[0]) != group.key.EmbeddingDimension {
			return domainerrors.ErrDimensionMismatch
		}
		group.queryVector = embedded.Vectors[0]
	}
	return nil
}

type replayFusedEntry struct {
	knowledgeBaseID uuid.UUID
	generationID    uuid.UUID
	candidate       FusedSearchCandidate
}

func (s *SearchReplayService) searchReplayPerKB(
	ctx context.Context,
	workspaceID uuid.UUID,
	override *searchSnapshotOverride,
	groups []replayEmbeddingGroup,
	query string,
) ([]replayFusedEntry, error) {
	// 找到每个 KB 对应的 query vector。
	kbVector := make(map[uuid.UUID][]float32)
	for _, group := range groups {
		for _, kbID := range group.kbIDs {
			kbVector[kbID] = group.queryVector
		}
	}
	var allEntries []replayFusedEntry
	err := s.repository.WithinWorkspace(ctx, workspaceID, func(txCtx context.Context, reader indexport.SearchReader) error {
		for kbID, gen := range override.generations {
			options, err := searchOptionsFromGeneration(gen, SearchInput{
				WorkspaceID: workspaceID, KnowledgeBaseID: kbID, Query: query,
				VectorTopK: &override.vectorTopK, KeywordTopK: &override.keywordTopK, FinalTopK: &override.finalTopK,
			})
			if err != nil {
				return err
			}
			request := indexport.SearchRequest{
				KnowledgeBaseID: kbID, GenerationID: gen.ID,
				Query: query, QueryEmbedding: kbVector[kbID],
				FTSConfig: options.ftsConfig, Dimension: gen.EmbeddingDimension,
				VectorTopK: options.vectorTopK, KeywordTopK: options.keywordTopK,
			}
			vectorCandidates, err := reader.VectorCandidates(txCtx, request)
			if err != nil {
				return err
			}
			keywordCandidates, err := reader.KeywordCandidates(txCtx, request)
			if err != nil {
				return err
			}
			rrfK := options.rrfK
			if rrfK <= 0 {
				rrfK = 60
			}
			fused := ReciprocalRankFusion(vectorCandidates, keywordCandidates, rrfK)
			for _, f := range fused {
				allEntries = append(allEntries, replayFusedEntry{
					knowledgeBaseID: kbID, generationID: gen.ID, candidate: f,
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return allEntries, nil
}

func mergeReplayFused(entries []replayFusedEntry) []replayMergedEntry {
	merged := make([]replayMergedEntry, 0, len(entries))
	for _, e := range entries {
		merged = append(merged, replayMergedEntry{
			knowledgeBaseID: e.knowledgeBaseID,
			generationID:    e.generationID,
			entryID:         e.candidate.EntryID,
			score:           e.candidate.Score,
			vectorScore:     e.candidate.VectorScore,
			keywordScore:    e.candidate.KeywordScore,
		})
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].score != merged[j].score {
			return merged[i].score > merged[j].score
		}
		if merged[i].knowledgeBaseID != merged[j].knowledgeBaseID {
			return merged[i].knowledgeBaseID.String() < merged[j].knowledgeBaseID.String()
		}
		return merged[i].entryID.String() < merged[j].entryID.String()
	})
	return merged
}

type replayMergedEntry struct {
	knowledgeBaseID uuid.UUID
	generationID    uuid.UUID
	entryID         uuid.UUID
	score           float64
	vectorScore     *float64
	keywordScore    *float64
}

func (s *SearchReplayService) loadReplayEvidence(
	ctx context.Context,
	workspaceID uuid.UUID,
	merged []replayMergedEntry,
	generations map[uuid.UUID]*model.IndexGeneration,
) ([]*dto.SearchResult, error) {
	if len(merged) == 0 {
		return []*dto.SearchResult{}, nil
	}
	type evidenceKey struct{ kb, gen uuid.UUID }
	buckets := make(map[evidenceKey][]replayMergedEntry)
	var keyOrder []evidenceKey
	for _, m := range merged {
		k := evidenceKey{kb: m.knowledgeBaseID, gen: m.generationID}
		if _, ok := buckets[k]; !ok {
			keyOrder = append(keyOrder, k)
		}
		buckets[k] = append(buckets[k], m)
	}
	results := make([]*dto.SearchResult, 0, len(merged))
	err := s.repository.WithinWorkspace(ctx, workspaceID, func(txCtx context.Context, reader indexport.SearchReader) error {
		for _, k := range keyOrder {
			bucket := buckets[k]
			entryIDs := make([]uuid.UUID, 0, len(bucket))
			for _, m := range bucket {
				entryIDs = append(entryIDs, m.entryID)
			}
			evidence, err := reader.LoadEvidence(txCtx, k.kb, k.gen, entryIDs)
			if err != nil {
				return err
			}
			byEntry := make(map[uuid.UUID]indexport.SearchEvidence, len(evidence))
			for _, ev := range evidence {
				byEntry[ev.EntryID] = ev
			}
			for _, m := range bucket {
				ev, ok := byEntry[m.entryID]
				if !ok {
					continue
				}
				result := dto.SearchResultFromEvidence(ev, k.gen, m.score, m.vectorScore, m.keywordScore)
				result.KnowledgeBaseID = m.knowledgeBaseID
				results = append(results, result)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].KnowledgeBaseID != results[j].KnowledgeBaseID {
			return results[i].KnowledgeBaseID.String() < results[j].KnowledgeBaseID.String()
		}
		return results[i].ChunkID.String() < results[j].ChunkID.String()
	})
	return results, nil
}
