package service

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

type multiSearchProfileStub struct{ snapshot *model.RerankSnapshot }

func (r *multiSearchProfileStub) Resolve(context.Context, uuid.UUID) (*model.RerankSnapshot, error) {
	return r.snapshot.Clone(), nil
}

type multiSearchRerankResolverStub struct {
	workspaceID uuid.UUID
	client      *ResolvedRerankClient
}

func (r *multiSearchRerankResolverStub) Resolve(_ context.Context, workspaceID, _ uuid.UUID) (*ResolvedRerankClient, error) {
	r.workspaceID = workspaceID
	return r.client, nil
}

// fakeMultiSearchRepository 记录 embedding 调用次数与每库候选。
type fakeMultiSearchRepository struct {
	mu              sync.Mutex
	activeGens      map[uuid.UUID]*model.IndexGeneration
	gensByID        map[uuid.UUID]*model.IndexGeneration
	vectorByKB      map[uuid.UUID][]indexport.SearchCandidate
	keywordByKB     map[uuid.UUID][]indexport.SearchCandidate
	evidenceByEntry map[uuid.UUID]indexport.SearchEvidence
	getActiveCalls  int
}

func (r *fakeMultiSearchRepository) WithinWorkspace(ctx context.Context, workspaceID uuid.UUID, fn func(context.Context, indexport.SearchReader) error) error {
	return fn(ctx, &fakeMultiSearchReader{repo: r})
}

type fakeMultiSearchReader struct{ repo *fakeMultiSearchRepository }

func (r *fakeMultiSearchReader) GetActiveGeneration(_ context.Context, kbID uuid.UUID) (*model.IndexGeneration, error) {
	r.repo.mu.Lock()
	defer r.repo.mu.Unlock()
	r.repo.getActiveCalls++
	return r.repo.activeGens[kbID], nil
}

func (r *fakeMultiSearchReader) GetGeneration(_ context.Context, _ uuid.UUID, generationID uuid.UUID) (*model.IndexGeneration, error) {
	r.repo.mu.Lock()
	defer r.repo.mu.Unlock()
	if g, ok := r.repo.gensByID[generationID]; ok {
		return g, nil
	}
	return nil, domainerrors.ErrNotFound
}
func (r *fakeMultiSearchReader) VectorCandidates(_ context.Context, req indexport.SearchRequest) ([]indexport.SearchCandidate, error) {
	r.repo.mu.Lock()
	defer r.repo.mu.Unlock()
	return r.repo.vectorByKB[req.KnowledgeBaseID], nil
}
func (r *fakeMultiSearchReader) KeywordCandidates(_ context.Context, req indexport.SearchRequest) ([]indexport.SearchCandidate, error) {
	r.repo.mu.Lock()
	defer r.repo.mu.Unlock()
	return r.repo.keywordByKB[req.KnowledgeBaseID], nil
}
func (r *fakeMultiSearchReader) LoadEvidence(_ context.Context, kbID, genID uuid.UUID, entryIDs []uuid.UUID) ([]indexport.SearchEvidence, error) {
	r.repo.mu.Lock()
	defer r.repo.mu.Unlock()
	out := make([]indexport.SearchEvidence, 0, len(entryIDs))
	for _, id := range entryIDs {
		if ev, ok := r.repo.evidenceByEntry[id]; ok {
			out = append(out, ev)
		}
	}
	return out, nil
}

// fakeMultiResolver 按 modelID 返回固定 ResolvedEmbeddingClient。
type fakeMultiResolver struct {
	byModel map[uuid.UUID]*ResolvedEmbeddingClient
}

func (r *fakeMultiResolver) Resolve(_ context.Context, _ uuid.UUID, modelID uuid.UUID) (*ResolvedEmbeddingClient, error) {
	return r.byModel[modelID], nil
}

// countingEmbeddingClient 记录 Embed 调用次数。
type countingEmbeddingClient struct {
	mu     sync.Mutex
	calls  int
	vector []float32
}

func (c *countingEmbeddingClient) Embed(_ context.Context, _ embeddingport.EmbedInput) (*embeddingport.EmbedResult, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return &embeddingport.EmbedResult{Vectors: [][]float32{c.vector}}, nil
}
func (c *countingEmbeddingClient) Dimension() int { return len(c.vector) }

func makeGeneration(workspaceID, kbID uuid.UUID, group embeddingGroupKey) *model.IndexGeneration {
	return &model.IndexGeneration{
		ID: uuid.New(), WorkspaceID: workspaceID, KnowledgeBaseID: kbID,
		EmbeddingModelID: group.EmbeddingModelID, ProviderID: group.ProviderID,
		ModelName: group.ModelName, EmbeddingDimension: group.EmbeddingDimension,
		ModelConfigHash: group.ModelConfigHash, Status: value.IndexGenerationReady,
		RetrievalConfig: map[string]any{
			"fts_config":   "english",
			"vector_top_k": 10, "keyword_top_k": 10, "final_top_k": 10, "rrf_k": 60,
		},
	}
}

func newMultiSearchFixture(groupKeys []embeddingGroupKey) (*MultiKnowledgeSearchService, *fakeMultiSearchRepository, *fakeMultiResolver) {
	repo := &fakeMultiSearchRepository{
		activeGens:      map[uuid.UUID]*model.IndexGeneration{},
		vectorByKB:      map[uuid.UUID][]indexport.SearchCandidate{},
		keywordByKB:     map[uuid.UUID][]indexport.SearchCandidate{},
		evidenceByEntry: map[uuid.UUID]indexport.SearchEvidence{},
	}
	resolver := &fakeMultiResolver{byModel: map[uuid.UUID]*ResolvedEmbeddingClient{}}
	for _, key := range groupKeys {
		vector := make([]float32, key.EmbeddingDimension)
		for i := range vector {
			vector[i] = 0.1
		}
		resolver.byModel[key.EmbeddingModelID] = &ResolvedEmbeddingClient{
			Client:  &countingEmbeddingClient{vector: vector},
			ModelID: key.EmbeddingModelID, ProviderID: key.ProviderID,
			ModelName: key.ModelName, Dimensions: key.EmbeddingDimension,
		}
	}
	names := &fakeAPIKeyNameStore{kbNames: map[uuid.UUID]string{}}
	svc := NewMultiKnowledgeSearchService(repo, resolver, nil, nil, names, config.SearchConfig{
		MultiKnowledgeBaseLimit: 20, MultiConcurrency: 4, MultiMergeRRFK: 60,
	}, nil, nil, 0)
	return svc, repo, resolver
}

func TestMultiSearchEmbedsOncePerSnapshotGroup(t *testing.T) {
	workspaceID := uuid.New()
	groupA := embeddingGroupKey{EmbeddingModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "model-a", EmbeddingDimension: 4, ModelConfigHash: "hash-a"}
	groupB := embeddingGroupKey{EmbeddingModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "model-b", EmbeddingDimension: 4, ModelConfigHash: "hash-b"}
	svc, repo, resolver := newMultiSearchFixture([]embeddingGroupKey{groupA, groupB})

	kbA1, kbA2, kbB1 := uuid.New(), uuid.New(), uuid.New()
	repo.activeGens[kbA1] = makeGeneration(workspaceID, kbA1, groupA)
	repo.activeGens[kbA2] = makeGeneration(workspaceID, kbA2, groupA)
	repo.activeGens[kbB1] = makeGeneration(workspaceID, kbB1, groupB)
	// 给每个 KB 一个候选。
	entryA1, entryA2, entryB1 := uuid.New(), uuid.New(), uuid.New()
	repo.vectorByKB[kbA1] = []indexport.SearchCandidate{{EntryID: entryA1, Score: 0.9}}
	repo.vectorByKB[kbA2] = []indexport.SearchCandidate{{EntryID: entryA2, Score: 0.8}}
	repo.vectorByKB[kbB1] = []indexport.SearchCandidate{{EntryID: entryB1, Score: 0.7}}
	repo.evidenceByEntry[entryA1] = indexport.SearchEvidence{EntryID: entryA1, ChunkID: uuid.New(), Content: "a1"}
	repo.evidenceByEntry[entryA2] = indexport.SearchEvidence{EntryID: entryA2, ChunkID: uuid.New(), Content: "a2"}
	repo.evidenceByEntry[entryB1] = indexport.SearchEvidence{EntryID: entryB1, ChunkID: uuid.New(), Content: "b1"}

	results, err := svc.Search(context.Background(), MultiKnowledgeSearchInput{
		WorkspaceID: workspaceID, Access: value.ResourceAccess{WorkspaceID: workspaceID, Unrestricted: true},
		KnowledgeBaseIDs: []uuid.UUID{kbA1, kbA2, kbB1}, Query: "如何重置密码？",
	})
	require.NoError(t, err)
	require.Len(t, results.Results, 3)
	// 2 个模型组各 embed 一次，共 2 次。
	require.Equal(t, 2, embedCallCount(resolver), "两个快照组应各 embed 一次")
	// 每条结果带 KB 来源。
	for _, r := range results.Results {
		require.NotEqual(t, uuid.Nil, r.KnowledgeBaseID)
	}
}

func TestMultiSearchDifferentEmbeddingsUseOneWorkspaceRerank(t *testing.T) {
	workspaceID := uuid.New()
	groupA := embeddingGroupKey{EmbeddingModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "model-a", EmbeddingDimension: 4, ModelConfigHash: "hash-a"}
	groupB := embeddingGroupKey{EmbeddingModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "model-b", EmbeddingDimension: 3, ModelConfigHash: "hash-b"}
	repo := &fakeMultiSearchRepository{activeGens: map[uuid.UUID]*model.IndexGeneration{}, vectorByKB: map[uuid.UUID][]indexport.SearchCandidate{}, keywordByKB: map[uuid.UUID][]indexport.SearchCandidate{}, evidenceByEntry: map[uuid.UUID]indexport.SearchEvidence{}}
	resolver := &fakeMultiResolver{byModel: map[uuid.UUID]*ResolvedEmbeddingClient{}}
	for _, group := range []embeddingGroupKey{groupA, groupB} {
		vector := make([]float32, group.EmbeddingDimension)
		resolver.byModel[group.EmbeddingModelID] = &ResolvedEmbeddingClient{Client: &countingEmbeddingClient{vector: vector}, ModelID: group.EmbeddingModelID, ProviderID: group.ProviderID, ModelName: group.ModelName, Dimensions: group.EmbeddingDimension}
	}
	names := &fakeAPIKeyNameStore{kbNames: map[uuid.UUID]string{}}
	rerankModelID, rerankProviderID := uuid.New(), uuid.New()
	rerankClient := &recordingRerankClientForSearch{scores: []float64{0.9, 0.8}}
	rerankResolver := &multiSearchRerankResolverStub{client: &ResolvedRerankClient{Client: rerankClient, ModelID: rerankModelID, ProviderID: rerankProviderID, ModelName: "rerank", ModelConfigHash: "rhash", MaxDocumentChars: 8192}}
	service := NewMultiKnowledgeSearchService(repo, resolver, rerankResolver, &multiSearchProfileStub{snapshot: &model.RerankSnapshot{ModelID: rerankModelID, ProviderID: rerankProviderID, ModelName: "rerank", ModelConfigHash: "rhash", CandidateTopK: 50, FailureMode: value.RerankFailureFail}}, names, config.SearchConfig{MultiKnowledgeBaseLimit: 20, MultiConcurrency: 4, MultiMergeRRFK: 60}, slog.Default(), nil, 0)
	kbA, kbB, entryA, entryB := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	repo.activeGens[kbA] = makeGeneration(workspaceID, kbA, groupA)
	repo.activeGens[kbB] = makeGeneration(workspaceID, kbB, groupB)
	repo.vectorByKB[kbA] = []indexport.SearchCandidate{{EntryID: entryA, Score: 0.9}}
	repo.vectorByKB[kbB] = []indexport.SearchCandidate{{EntryID: entryB, Score: 0.8}}
	repo.evidenceByEntry[entryA] = indexport.SearchEvidence{EntryID: entryA, ChunkID: uuid.New(), Content: "候选 A", SearchContent: "候选 A"}
	repo.evidenceByEntry[entryB] = indexport.SearchEvidence{EntryID: entryB, ChunkID: uuid.New(), Content: "候选 B", SearchContent: "候选 B"}
	results, err := service.Search(context.Background(), MultiKnowledgeSearchInput{WorkspaceID: workspaceID, Access: value.ResourceAccess{WorkspaceID: workspaceID, Unrestricted: true}, KnowledgeBaseIDs: []uuid.UUID{kbA, kbB}, Query: "跨模型查询"})
	if err != nil || len(results.Results) != 2 {
		t.Fatalf("results=%#v err=%v", results, err)
	}
	if rerankResolver.workspaceID != workspaceID || rerankClient.calls != 1 || rerankClient.input.Query != "跨模型查询" {
		t.Fatalf("workspace=%s calls=%d query=%q", rerankResolver.workspaceID, rerankClient.calls, rerankClient.input.Query)
	}
}

func TestMultiSearchGroupsMatchingChildrenBeforeFinalTopK(t *testing.T) {
	workspaceID := uuid.New()
	group := embeddingGroupKey{EmbeddingModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "model", EmbeddingDimension: 4, ModelConfigHash: "hash"}
	svc, repo, _ := newMultiSearchFixture([]embeddingGroupKey{group})
	kbID, entryA, entryB, parentID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	repo.activeGens[kbID] = makeGeneration(workspaceID, kbID, group)
	repo.vectorByKB[kbID] = []indexport.SearchCandidate{{EntryID: entryA, Score: 0.9}, {EntryID: entryB, Score: 0.8}}
	repo.evidenceByEntry[entryA] = indexport.SearchEvidence{
		EntryID: entryA, ChunkID: parentID, ChunkRevisionID: uuid.New(), Content: "完整父块",
		MatchedChunkID: uuid.New(), MatchedChunkRevisionID: uuid.New(), MatchedContent: "命中子块 A", MatchedRole: value.ChunkRoleChild,
	}
	repo.evidenceByEntry[entryB] = indexport.SearchEvidence{
		EntryID: entryB, ChunkID: parentID, ChunkRevisionID: uuid.New(), Content: "完整父块",
		MatchedChunkID: uuid.New(), MatchedChunkRevisionID: uuid.New(), MatchedContent: "命中子块 B", MatchedRole: value.ChunkRoleChild,
	}

	results, err := svc.Search(context.Background(), MultiKnowledgeSearchInput{
		WorkspaceID: workspaceID, Access: value.ResourceAccess{WorkspaceID: workspaceID, Unrestricted: true},
		KnowledgeBaseIDs: []uuid.UUID{kbID}, Query: "配置",
	})
	require.NoError(t, err)
	require.Len(t, results.Results, 1)
	require.Equal(t, parentID, results.Results[0].ChunkID)
	require.Len(t, results.Results[0].MatchedChildren, 2)
}

// embedCallCount 统计所有 countingEmbeddingClient 的 embed 次数。
func embedCallCount(resolver *fakeMultiResolver) int {
	total := 0
	for _, rc := range resolver.byModel {
		if rec, ok := rc.Client.(*countingEmbeddingClient); ok {
			rec.mu.Lock()
			total += rec.calls
			rec.mu.Unlock()
		}
	}
	return total
}

func TestMultiSearchRejectsUnboundKBBeforeEmbedding(t *testing.T) {
	workspaceID := uuid.New()
	groupA := embeddingGroupKey{EmbeddingModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "model-a", EmbeddingDimension: 4, ModelConfigHash: "hash-a"}
	svc, repo, _ := newMultiSearchFixture([]embeddingGroupKey{groupA})
	boundKB := uuid.New()
	unboundKB := uuid.New()
	repo.activeGens[boundKB] = makeGeneration(workspaceID, boundKB, groupA)
	repo.activeGens[unboundKB] = makeGeneration(workspaceID, unboundKB, groupA)

	_, err := svc.Search(context.Background(), MultiKnowledgeSearchInput{
		WorkspaceID:      workspaceID,
		Access:           value.ResourceAccess{WorkspaceID: workspaceID, AllowedKnowledgeBaseIDs: []uuid.UUID{boundKB}},
		KnowledgeBaseIDs: []uuid.UUID{boundKB, unboundKB}, Query: "q",
	})
	require.Error(t, err, "越界知识库应被拒绝")
	// 不应触发 embedding（embedding 次数为 0）。
}

func TestMultiSearchRejectsEmptyAndTooManyKBs(t *testing.T) {
	svc, _, _ := newMultiSearchFixture(nil)
	_, err := svc.Search(context.Background(), MultiKnowledgeSearchInput{
		WorkspaceID: uuid.New(), Access: value.ResourceAccess{Unrestricted: true},
		KnowledgeBaseIDs: nil, Query: "q",
	})
	require.Error(t, err)

	ids := make([]uuid.UUID, 25)
	for i := range ids {
		ids[i] = uuid.New()
	}
	_, err = svc.Search(context.Background(), MultiKnowledgeSearchInput{
		WorkspaceID: uuid.New(), Access: value.ResourceAccess{Unrestricted: true},
		KnowledgeBaseIDs: ids, Query: "q",
	})
	require.Error(t, err)
}

func TestMultiSearchRejectsEmptyQuery(t *testing.T) {
	svc, _, _ := newMultiSearchFixture(nil)
	_, err := svc.Search(context.Background(), MultiKnowledgeSearchInput{
		WorkspaceID: uuid.New(), Access: value.ResourceAccess{Unrestricted: true},
		KnowledgeBaseIDs: []uuid.UUID{uuid.New()}, Query: "  ",
	})
	require.Error(t, err)
}

func TestMultiSearchRecordsEffectiveScope(t *testing.T) {
	workspaceID := uuid.New()
	group := embeddingGroupKey{EmbeddingModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "model", EmbeddingDimension: 4, ModelConfigHash: "hash"}
	repo := &fakeMultiSearchRepository{
		activeGens: map[uuid.UUID]*model.IndexGeneration{}, vectorByKB: map[uuid.UUID][]indexport.SearchCandidate{}, keywordByKB: map[uuid.UUID][]indexport.SearchCandidate{}, evidenceByEntry: map[uuid.UUID]indexport.SearchEvidence{}, gensByID: map[uuid.UUID]*model.IndexGeneration{},
	}
	resolver := &fakeMultiResolver{byModel: map[uuid.UUID]*ResolvedEmbeddingClient{}}
	vector := make([]float32, group.EmbeddingDimension)
	resolver.byModel[group.EmbeddingModelID] = &ResolvedEmbeddingClient{Client: &countingEmbeddingClient{vector: vector}, ModelID: group.EmbeddingModelID, ProviderID: group.ProviderID, ModelName: group.ModelName, Dimensions: group.EmbeddingDimension}
	names := &fakeAPIKeyNameStore{kbNames: map[uuid.UUID]string{}}
	store := &fakeSearchRunStore{}
	svc := NewMultiKnowledgeSearchService(repo, resolver, nil, nil, names, config.SearchConfig{
		MultiKnowledgeBaseLimit: 20, MultiConcurrency: 4, MultiMergeRRFK: 60,
	}, nil, store, 0)
	kbA, kbB := uuid.New(), uuid.New()
	repo.activeGens[kbA] = makeGeneration(workspaceID, kbA, group)
	repo.activeGens[kbB] = makeGeneration(workspaceID, kbB, group)
	entryA, entryB := uuid.New(), uuid.New()
	repo.vectorByKB[kbA] = []indexport.SearchCandidate{{EntryID: entryA, Score: 0.9}}
	repo.vectorByKB[kbB] = []indexport.SearchCandidate{{EntryID: entryB, Score: 0.8}}
	repo.evidenceByEntry[entryA] = indexport.SearchEvidence{EntryID: entryA, ChunkID: uuid.New(), Content: "A"}
	repo.evidenceByEntry[entryB] = indexport.SearchEvidence{EntryID: entryB, ChunkID: uuid.New(), Content: "B"}

	response, err := svc.Search(context.Background(), MultiKnowledgeSearchInput{
		WorkspaceID: workspaceID, Access: value.ResourceAccess{WorkspaceID: workspaceID, Unrestricted: true},
		KnowledgeBaseIDs: []uuid.UUID{kbA, kbB}, Query: "退款", RequestedScope: value.SearchScopeSelected,
	})
	require.NoError(t, err)
	require.NotNil(t, response)
	require.ElementsMatch(t, []uuid.UUID{kbA, kbB}, response.Run.EffectiveKnowledgeBaseIDs)
	require.Len(t, response.Run.GenerationSnapshots, 2)
	require.NotEqual(t, uuid.Nil, response.Run.SearchID)
	require.Equal(t, value.SearchScopeSelected, response.Run.RequestedScope)
}

func TestMultiSearchRequestedScopeDefaultsToSelected(t *testing.T) {
	workspaceID := uuid.New()
	group := embeddingGroupKey{EmbeddingModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "model", EmbeddingDimension: 4, ModelConfigHash: "hash"}
	repo := &fakeMultiSearchRepository{
		activeGens: map[uuid.UUID]*model.IndexGeneration{}, vectorByKB: map[uuid.UUID][]indexport.SearchCandidate{}, keywordByKB: map[uuid.UUID][]indexport.SearchCandidate{}, evidenceByEntry: map[uuid.UUID]indexport.SearchEvidence{}, gensByID: map[uuid.UUID]*model.IndexGeneration{},
	}
	resolver := &fakeMultiResolver{byModel: map[uuid.UUID]*ResolvedEmbeddingClient{}}
	vector := make([]float32, group.EmbeddingDimension)
	resolver.byModel[group.EmbeddingModelID] = &ResolvedEmbeddingClient{Client: &countingEmbeddingClient{vector: vector}, ModelID: group.EmbeddingModelID, ProviderID: group.ProviderID, ModelName: group.ModelName, Dimensions: group.EmbeddingDimension}
	names := &fakeAPIKeyNameStore{kbNames: map[uuid.UUID]string{}}
	store := &fakeSearchRunStore{}
	svc := NewMultiKnowledgeSearchService(repo, resolver, nil, nil, names, config.SearchConfig{
		MultiKnowledgeBaseLimit: 20, MultiConcurrency: 4, MultiMergeRRFK: 60,
	}, nil, store, 0)
	kbID := uuid.New()
	repo.activeGens[kbID] = makeGeneration(workspaceID, kbID, group)
	entry := uuid.New()
	repo.vectorByKB[kbID] = []indexport.SearchCandidate{{EntryID: entry, Score: 0.9}}
	repo.evidenceByEntry[entry] = indexport.SearchEvidence{EntryID: entry, ChunkID: uuid.New(), Content: "X"}

	response, err := svc.Search(context.Background(), MultiKnowledgeSearchInput{
		WorkspaceID: workspaceID, Access: value.ResourceAccess{WorkspaceID: workspaceID, Unrestricted: true},
		KnowledgeBaseIDs: []uuid.UUID{kbID}, Query: "退款",
	})
	require.NoError(t, err)
	require.Equal(t, value.SearchScopeSelected, response.Run.RequestedScope)
}

func TestEmbeddingGroupKeyGrouping(t *testing.T) {
	kb1, kb2, kb3 := uuid.New(), uuid.New(), uuid.New()
	groupA := embeddingGroupKey{EmbeddingModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "m", EmbeddingDimension: 4, ModelConfigHash: "h-a"}
	groupB := embeddingGroupKey{EmbeddingModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "m", EmbeddingDimension: 4, ModelConfigHash: "h-b"}
	snapshots := map[uuid.UUID]knowledgeBaseSearchSnapshot{
		kb1: {knowledgeBaseID: kb1, generation: makeGeneration(uuid.New(), kb1, groupA)},
		kb2: {knowledgeBaseID: kb2, generation: makeGeneration(uuid.New(), kb2, groupA)},
		kb3: {knowledgeBaseID: kb3, generation: makeGeneration(uuid.New(), kb3, groupB)},
	}
	groups := groupByEmbeddingSnapshot(snapshots)
	require.Len(t, groups, 2, "两个不同快照应分成两组")
	// 组 A 含两个 KB，组 B 含一个 KB。
	var groupASize, groupBSize int
	for _, g := range groups {
		if g.key.ModelConfigHash == "h-a" {
			groupASize = len(g.members)
		}
		if g.key.ModelConfigHash == "h-b" {
			groupBSize = len(g.members)
		}
	}
	require.Equal(t, 2, groupASize)
	require.Equal(t, 1, groupBSize)
}

func TestMergeAcrossKnowledgeBasesStableTieBreak(t *testing.T) {
	kb1 := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	kb2 := uuid.MustParse("00000000-0000-4000-8000-000000000002")
	entry := uuid.New()
	// 相同 score 时按 KB ID 升序。
	entries := []perKBFusedEntry{
		{knowledgeBaseID: kb2, candidate: FusedSearchCandidate{EntryID: entry, Score: 0.5}},
		{knowledgeBaseID: kb1, candidate: FusedSearchCandidate{EntryID: entry, Score: 0.5}},
	}
	merged := mergeAcrossKnowledgeBases(entries)
	require.Equal(t, kb1, merged[0].knowledgeBaseID)
	require.Equal(t, kb2, merged[1].knowledgeBaseID)
}
