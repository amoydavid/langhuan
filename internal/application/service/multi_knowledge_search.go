package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

// embeddingGroupKey 是不可变模型身份五元组，任一字段不同都不得复用向量。
type embeddingGroupKey struct {
	EmbeddingModelID   uuid.UUID
	ProviderID         uuid.UUID
	ModelName          string
	EmbeddingDimension int
	ModelConfigHash    string
}

// MultiKnowledgeSearchInput 是多知识库检索的协议中立输入。
type MultiKnowledgeSearchInput struct {
	WorkspaceID      uuid.UUID
	Access           value.ResourceAccess
	KnowledgeBaseIDs []uuid.UUID
	Query            string
	VectorTopK       *int
	KeywordTopK      *int
	FinalTopK        *int
}

// knowledgeBaseSearchSnapshot 描述一个被选中知识库在检索开始时的只读快照。
type knowledgeBaseSearchSnapshot struct {
	knowledgeBaseID uuid.UUID
	name            string
	generation      *model.IndexGeneration
}

// embeddingGroup 是同一模型快照下的一组知识库，共享一次 query embedding。
type embeddingGroup struct {
	key         embeddingGroupKey
	queryVector []float32
	members     []knowledgeBaseSearchSnapshot
}

// MultiKnowledgeSearchService 协调跨多个知识库的混合检索。
//
// 执行顺序固定为：trim/query 校验 -> KB IDs 去重且 1..limit -> 在一个
// Workspace read 中加载全部 KB 名称和 ready active Generation -> 全部 access
// 校验 -> 按五元组分组 -> errgroup + semaphore 每组 resolve/一次 embed ->
// 每 KB 用共同 multi_merge_rrf_k 做 vector/keyword RRF -> 合并 model-
// independent score -> 加载完整 evidence -> 按父块聚合 -> 稳定排序与全局截断。
type MultiKnowledgeSearchService struct {
	repository       indexport.SearchRepository
	resolver         EmbeddingClientResolver
	names            APIKeyNameStore
	multiLimit       int
	multiConcurrency int
	mergeRRFK        int
}

// NewMultiKnowledgeSearchService 构造多知识库检索服务。
func NewMultiKnowledgeSearchService(
	repository indexport.SearchRepository,
	resolver EmbeddingClientResolver,
	names APIKeyNameStore,
	cfg config.SearchConfig,
) *MultiKnowledgeSearchService {
	limit := cfg.MultiKnowledgeBaseLimit
	if limit < 1 {
		limit = 20
	}
	concurrency := cfg.MultiConcurrency
	if concurrency < 1 {
		concurrency = 4
	}
	rrfK := cfg.MultiMergeRRFK
	if rrfK <= 0 {
		rrfK = 60
	}
	return &MultiKnowledgeSearchService{
		repository: repository, resolver: resolver, names: names,
		multiLimit: limit, multiConcurrency: concurrency, mergeRRFK: rrfK,
	}
}

// Search 执行多知识库检索。任一 Generation 无效、Provider 失败、维度不匹配或查询
// 失败都按 all-or-nothing 返回稳定错误。
func (s *MultiKnowledgeSearchService) Search(ctx context.Context, input MultiKnowledgeSearchInput) ([]*dto.SearchResult, error) {
	if s.repository == nil || s.resolver == nil || s.names == nil {
		return nil, fmt.Errorf("%w: 多知识库检索依赖不能为空", domainerrors.ErrValidation)
	}
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return nil, fmt.Errorf("%w: 检索 query 不能为空", domainerrors.ErrValidation)
	}
	if input.WorkspaceID == uuid.Nil {
		return nil, fmt.Errorf("%w: workspace_id 不能为空", domainerrors.ErrValidation)
	}
	kbIDs := dedupeUUIDs(input.KnowledgeBaseIDs)
	if len(kbIDs) == 0 {
		return nil, fmt.Errorf("%w: 至少选择一个知识库", domainerrors.ErrValidation)
	}
	if len(kbIDs) > s.multiLimit {
		return nil, fmt.Errorf("%w: 最多检索 %d 个知识库", domainerrors.ErrValidation, s.multiLimit)
	}
	// 全部 access 校验：API Key 只能检索绑定的知识库；Session 不受限。
	for _, kbID := range kbIDs {
		if !input.Access.AllowsKnowledgeBase(kbID) {
			// 越界统一 not_found，不泄漏存在性。
			return nil, domainerrors.ErrNotFound
		}
	}

	finalTopK := dereferenceInt(input.FinalTopK, maxFinalTopK)
	if finalTopK < 1 {
		finalTopK = maxFinalTopK
	}
	if finalTopK > maxFinalTopK {
		finalTopK = maxFinalTopK
	}

	// 在一个 Workspace read 中加载全部 KB 名称和 ready active Generation。
	snapshots, err := s.loadSnapshots(ctx, input.WorkspaceID, kbIDs)
	if err != nil {
		return nil, err
	}

	// 按五元组分组。
	groups := groupByEmbeddingSnapshot(snapshots)

	// 并发解析 + 一次 embed 每组。任一组失败全部失败。
	if err := s.embedGroups(ctx, input.WorkspaceID, groups, query); err != nil {
		return nil, err
	}

	// 在一个 Workspace tx 内对每个 KB 做 vector/keyword 检索与 RRF。
	perKBFused, err := s.searchPerKB(ctx, input, groups, query)
	if err != nil {
		return nil, err
	}

	// 全局稳定合并：score DESC, knowledge_base_id ASC, chunk_id ASC。
	merged := mergeAcrossKnowledgeBases(perKBFused)

	// 先加载 evidence 才能识别 child 对应的 parent；全局截断必须发生在
	// parent 聚合之后，避免同一父块的多个命中子块占用结果名额。
	results, err := s.loadEvidenceAndBuild(ctx, input.WorkspaceID, merged, snapshots)
	if err != nil {
		return nil, err
	}
	results = groupMultiSearchResults(results)
	if finalTopK < len(results) {
		results = results[:finalTopK]
	}
	return results, nil
}

// loadSnapshots 在一个 Workspace read 内加载每个 KB 的名称和 ready active Generation。
func (s *MultiKnowledgeSearchService) loadSnapshots(ctx context.Context, workspaceID uuid.UUID, kbIDs []uuid.UUID) (map[uuid.UUID]knowledgeBaseSearchSnapshot, error) {
	names, err := s.names.KnowledgeBaseNames(ctx, workspaceID, kbIDs)
	if err != nil {
		return nil, err
	}
	snapshots := make(map[uuid.UUID]knowledgeBaseSearchSnapshot, len(kbIDs))
	if err := s.repository.WithinWorkspace(ctx, workspaceID, func(txCtx context.Context, reader indexport.SearchReader) error {
		for _, kbID := range kbIDs {
			gen, err := reader.GetActiveGeneration(txCtx, kbID)
			if err != nil {
				return err
			}
			if gen == nil || gen.WorkspaceID != workspaceID || gen.KnowledgeBaseID != kbID {
				return domainerrors.ErrGenerationNotReady
			}
			if gen.Status != value.IndexGenerationReady {
				return domainerrors.ErrGenerationNotReady
			}
			snapshots[kbID] = knowledgeBaseSearchSnapshot{knowledgeBaseID: kbID, name: names[kbID], generation: gen}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return snapshots, nil
}

// embedGroups 并发地为每个模型组解析 embedding 客户端并生成一次 query 向量。
// 所有组内 KB 共享同一 query 文本；每组只 embed 一次。
func (s *MultiKnowledgeSearchService) embedGroups(ctx context.Context, workspaceID uuid.UUID, groups []embeddingGroup, query string) error {
	g, groupCtx := errgroup.WithContext(ctx)
	sem := make(chan struct{}, s.multiConcurrency)
	for i := range groups {
		group := &groups[i]
		sem <- struct{}{}
		g.Go(func() error {
			defer func() { <-sem }()
			resolved, err := s.resolver.Resolve(groupCtx, workspaceID, group.key.EmbeddingModelID)
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
			embedded, err := resolved.Client.Embed(groupCtx, embeddingPortEmbedInput(query))
			if err != nil {
				return err
			}
			if len(embedded.Vectors) != 1 || len(embedded.Vectors[0]) != group.key.EmbeddingDimension {
				return domainerrors.ErrDimensionMismatch
			}
			group.queryVector = embedded.Vectors[0]
			return nil
		})
	}
	return g.Wait()
}

// embeddingPortEmbedInput 构造单条 query 的 embedding 输入。
func embeddingPortEmbedInput(query string) embeddingport.EmbedInput {
	return embeddingport.EmbedInput{Texts: []string{query}}
}

func groupByEmbeddingSnapshot(snapshots map[uuid.UUID]knowledgeBaseSearchSnapshot) []embeddingGroup {
	byKey := make(map[embeddingGroupKey][]knowledgeBaseSearchSnapshot)
	var keyOrder []embeddingGroupKey
	for _, snap := range snapshots {
		key := embeddingGroupKey{
			EmbeddingModelID:   snap.generation.EmbeddingModelID,
			ProviderID:         snap.generation.ProviderID,
			ModelName:          snap.generation.ModelName,
			EmbeddingDimension: snap.generation.EmbeddingDimension,
			ModelConfigHash:    snap.generation.ModelConfigHash,
		}
		if _, ok := byKey[key]; !ok {
			keyOrder = append(keyOrder, key)
		}
		byKey[key] = append(byKey[key], snap)
	}
	sort.Slice(keyOrder, func(i, j int) bool {
		return keyOrder[i].EmbeddingModelID.String() < keyOrder[j].EmbeddingModelID.String()
	})
	groups := make([]embeddingGroup, 0, len(keyOrder))
	for _, key := range keyOrder {
		members := byKey[key]
		sort.Slice(members, func(i, j int) bool { return members[i].knowledgeBaseID.String() < members[j].knowledgeBaseID.String() })
		groups = append(groups, embeddingGroup{key: key, members: members})
	}
	return groups
}

// perKBFusedEntry 是单库 RRF 后的候选，附带 KB 来源。
type perKBFusedEntry struct {
	knowledgeBaseID   uuid.UUID
	knowledgeBaseName string
	generationID      uuid.UUID
	candidate         FusedSearchCandidate
}

func (s *MultiKnowledgeSearchService) searchPerKB(ctx context.Context, input MultiKnowledgeSearchInput, groups []embeddingGroup, query string) ([]perKBFusedEntry, error) {
	var allEntries []perKBFusedEntry
	err := s.repository.WithinWorkspace(ctx, input.WorkspaceID, func(txCtx context.Context, reader indexport.SearchReader) error {
		for _, group := range groups {
			for _, snap := range group.members {
				// 重新确认 active Generation 未切换。
				current, err := reader.GetActiveGeneration(txCtx, snap.knowledgeBaseID)
				if err != nil {
					return err
				}
				if current == nil || current.ID != snap.generation.ID {
					return domainerrors.ErrGenerationStale
				}
				options, err := searchOptionsFromGeneration(snap.generation, SearchInput{
					WorkspaceID: input.WorkspaceID, KnowledgeBaseID: snap.knowledgeBaseID,
					Query: query, VectorTopK: input.VectorTopK, KeywordTopK: input.KeywordTopK, FinalTopK: input.FinalTopK,
				})
				if err != nil {
					return err
				}
				request := indexport.SearchRequest{
					KnowledgeBaseID: snap.knowledgeBaseID, GenerationID: snap.generation.ID,
					Query: query, QueryEmbedding: group.queryVector,
					FTSConfig:   options.ftsConfig,
					Dimension:   snap.generation.EmbeddingDimension,
					VectorTopK:  options.vectorTopK,
					KeywordTopK: options.keywordTopK,
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
					rrfK = s.mergeRRFK
				}
				fused := ReciprocalRankFusion(vectorCandidates, keywordCandidates, rrfK)
				for _, f := range fused {
					allEntries = append(allEntries, perKBFusedEntry{
						knowledgeBaseID: snap.knowledgeBaseID, knowledgeBaseName: snap.name,
						generationID: snap.generation.ID, candidate: f,
					})
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return allEntries, nil
}

// mergedEntry 是全局合并后的候选。
type mergedEntry struct {
	knowledgeBaseID   uuid.UUID
	knowledgeBaseName string
	generationID      uuid.UUID
	entryID           uuid.UUID
	score             float64
	vectorScore       *float64
	keywordScore      *float64
}

func mergeAcrossKnowledgeBases(entries []perKBFusedEntry) []mergedEntry {
	merged := make([]mergedEntry, 0, len(entries))
	for _, e := range entries {
		merged = append(merged, mergedEntry{
			knowledgeBaseID: e.knowledgeBaseID, knowledgeBaseName: e.knowledgeBaseName,
			generationID: e.generationID, entryID: e.candidate.EntryID,
			score: e.candidate.Score, vectorScore: e.candidate.VectorScore, keywordScore: e.candidate.KeywordScore,
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

func (s *MultiKnowledgeSearchService) loadEvidenceAndBuild(ctx context.Context, workspaceID uuid.UUID, merged []mergedEntry, snapshots map[uuid.UUID]knowledgeBaseSearchSnapshot) ([]*dto.SearchResult, error) {
	if len(merged) == 0 {
		return []*dto.SearchResult{}, nil
	}
	// 按 (KB, generation) 分组加载 evidence。
	type evidenceKey struct{ kb, gen uuid.UUID }
	buckets := make(map[evidenceKey][]mergedEntry)
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
				result := dto.SearchResultFromEvidence(ev, m.score, m.vectorScore, m.keywordScore)
				result.KnowledgeBaseID = m.knowledgeBaseID
				result.KnowledgeBaseName = m.knowledgeBaseName
				results = append(results, result)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// 保持全局排序（results 已按 keyOrder 顺序追加，但需重排以恢复全局排序）。
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

// groupMultiSearchResults merges hits for the same effective parent within one KB.
// LoadEvidence resolves a flat chunk to itself, so this also retains flat semantics.
func groupMultiSearchResults(results []*dto.SearchResult) []*dto.SearchResult {
	type groupKey struct {
		knowledgeBaseID uuid.UUID
		chunkID         uuid.UUID
	}
	grouped := make(map[groupKey]*dto.SearchResult, len(results))
	for _, current := range results {
		if current == nil {
			continue
		}
		key := groupKey{knowledgeBaseID: current.KnowledgeBaseID, chunkID: current.ChunkID}
		prior := grouped[key]
		if prior == nil {
			grouped[key] = current
			continue
		}
		if current.Score > prior.Score {
			matched := prior.MatchedChildren
			*prior = *current
			prior.MatchedChildren = matched
		}
		prior.MatchedChildren = append(prior.MatchedChildren, current.MatchedChildren...)
	}
	merged := make([]*dto.SearchResult, 0, len(grouped))
	for _, result := range grouped {
		sort.Slice(result.MatchedChildren, func(i, j int) bool {
			if result.MatchedChildren[i].Score != result.MatchedChildren[j].Score {
				return result.MatchedChildren[i].Score > result.MatchedChildren[j].Score
			}
			return result.MatchedChildren[i].ChunkID.String() < result.MatchedChildren[j].ChunkID.String()
		})
		merged = append(merged, result)
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].Score != merged[j].Score {
			return merged[i].Score > merged[j].Score
		}
		if merged[i].KnowledgeBaseID != merged[j].KnowledgeBaseID {
			return merged[i].KnowledgeBaseID.String() < merged[j].KnowledgeBaseID.String()
		}
		return merged[i].ChunkID.String() < merged[j].ChunkID.String()
	})
	return merged
}

func dedupeUUIDs(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]bool, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func dereferenceInt(p *int, fallback int) int {
	if p != nil && *p >= 1 {
		return *p
	}
	return fallback
}
