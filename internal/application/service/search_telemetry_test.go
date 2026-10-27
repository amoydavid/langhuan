package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/domain/value"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

// kbFlagsStub 控制 query_log 开关读取。
type kbFlagsStub struct {
	enabled map[uuid.UUID]bool
	err     error
	calls   int
}

func (f *kbFlagsStub) GetQueryLogEnabled(_ context.Context, _, kbID uuid.UUID) (bool, error) {
	f.calls++
	if f.err != nil {
		return false, f.err
	}
	return f.enabled[kbID], nil
}

func telemetryTestRepo(t *testing.T) (*searchRepositoryFake, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
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
	return repository, workspaceID, knowledgeBaseID, generation.ID
}

func TestSearchDeliversTelemetryAfterFinish(t *testing.T) {
	repo, workspaceID, kbID, _ := telemetryTestRepo(t)
	store := &fakeTelemetryStore{}
	flags := &kbFlagsStub{enabled: map[uuid.UUID]bool{kbID: true}}
	recorder := NewCitationRecorder(store, CitationRecorderConfig{
		BufferSize: 8, FlushInterval: 5 * time.Millisecond, FlushBatchSize: 100,
	}, nil)

	service := NewSearchService(SearchServiceDeps{
		Repository: repo, Resolver: searchRunTestResolver(repo.generation.EmbeddingModelID, repo.generation.ProviderID),
		SearchRuns: &fakeSearchRunStore{}, Telemetry: recorder, KBFlags: flags,
	})
	response, err := service.Search(context.Background(), SearchInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: kbID, Query: "退款政策是什么",
	})
	require.NoError(t, err)
	require.Equal(t, value.RetrievalStatusAvailable, response.Run.RetrievalStatus)

	// 关闭 recorder 触发残量 flush。
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	recorder.Close(ctx)

	citations, logs, _ := store.snapshot()
	require.Len(t, citations, 1, "available run 的结果行应投递引用遥测")
	require.Equal(t, workspaceID, citations[0].WorkspaceID)
	require.Equal(t, kbID, citations[0].KbID)
	require.Equal(t, 1, citations[0].Rank)
	require.Equal(t, response.Run.SearchID, citations[0].SearchRunID)
	require.Len(t, logs, 1, "开关开启时应投递查询原文")
	require.Equal(t, "退款政策是什么", logs[0].QueryText)
	require.Equal(t, response.Run.QueryHash, logs[0].QueryHash)
}

func TestSearchTelemetryStoreFailureDoesNotAffectResponse(t *testing.T) {
	repo, workspaceID, kbID, _ := telemetryTestRepo(t)
	// 落库必失败 + 开关读取必失败的遥测链路。
	store := &fakeTelemetryStore{insertErr: errors.New("db down")}
	flags := &kbFlagsStub{err: errors.New("flag read down")}
	recorder := NewCitationRecorder(store, CitationRecorderConfig{
		BufferSize: 8, FlushInterval: 5 * time.Millisecond, FlushBatchSize: 100,
	}, nil)

	telemetryService := NewSearchService(SearchServiceDeps{
		Repository: repo, Resolver: searchRunTestResolver(repo.generation.EmbeddingModelID, repo.generation.ProviderID),
		SearchRuns: &fakeSearchRunStore{}, Telemetry: recorder, KBFlags: flags,
	})
	withTelemetry, err := telemetryService.Search(context.Background(), SearchInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: kbID, Query: "退款政策是什么",
	})
	require.NoError(t, err, "遥测落库失败不得让检索失败")

	// 同一输入在无遥测下的响应应逐位一致（除 run ID/时间戳外）。
	bareService := NewSearchService(SearchServiceDeps{
		Repository: repo, Resolver: searchRunTestResolver(repo.generation.EmbeddingModelID, repo.generation.ProviderID),
		SearchRuns: &fakeSearchRunStore{},
	})
	bare, err := bareService.Search(context.Background(), SearchInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: kbID, Query: "退款政策是什么",
	})
	require.NoError(t, err)

	require.Equal(t, bare.Run.RetrievalStatus, withTelemetry.Run.RetrievalStatus)
	require.Equal(t, bare.Run.ResultCount, withTelemetry.Run.ResultCount)
	require.Equal(t, len(bare.Results), len(withTelemetry.Results))
	for i := range bare.Results {
		require.Equal(t, bare.Results[i].ChunkID, withTelemetry.Results[i].ChunkID)
		require.Equal(t, bare.Results[i].Score, withTelemetry.Results[i].Score)
		require.Equal(t, bare.Results[i].Citation.ContentSHA256, withTelemetry.Results[i].Citation.ContentSHA256)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	recorder.Close(ctx)
}

func TestSearchDoesNotRecordQueryTextWhenDisabled(t *testing.T) {
	repo, workspaceID, kbID, _ := telemetryTestRepo(t)
	store := &fakeTelemetryStore{}
	flags := &kbFlagsStub{enabled: map[uuid.UUID]bool{kbID: false}}
	recorder := NewCitationRecorder(store, CitationRecorderConfig{
		BufferSize: 8, FlushInterval: 5 * time.Millisecond, FlushBatchSize: 100,
	}, nil)

	service := NewSearchService(SearchServiceDeps{
		Repository: repo, Resolver: searchRunTestResolver(repo.generation.EmbeddingModelID, repo.generation.ProviderID),
		SearchRuns: &fakeSearchRunStore{}, Telemetry: recorder, KBFlags: flags,
	})
	_, err := service.Search(context.Background(), SearchInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: kbID, Query: "内部问题",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	recorder.Close(ctx)

	citations, logs, _ := store.snapshot()
	require.Len(t, citations, 1, "引用遥测与明文开关独立，仍应投递")
	require.Empty(t, logs, "开关关闭时不得投递明文")
}

func TestSearchEmptyRunDeliversOnlyQueryLog(t *testing.T) {
	workspaceID, knowledgeBaseID := uuid.New(), uuid.New()
	generation := searchRunTestGeneration(workspaceID, knowledgeBaseID)
	repository := &searchRepositoryFake{
		generation: generation, vector: nil, keyword: nil, evidence: map[uuid.UUID]indexport.SearchEvidence{},
	}
	store := &fakeTelemetryStore{}
	flags := &kbFlagsStub{enabled: map[uuid.UUID]bool{knowledgeBaseID: true}}
	recorder := NewCitationRecorder(store, CitationRecorderConfig{
		BufferSize: 8, FlushInterval: 5 * time.Millisecond, FlushBatchSize: 100,
	}, nil)

	service := NewSearchService(SearchServiceDeps{
		Repository: repository, Resolver: searchRunTestResolver(generation.EmbeddingModelID, generation.ProviderID),
		SearchRuns: &fakeSearchRunStore{}, Telemetry: recorder, KBFlags: flags,
	})
	response, err := service.Search(context.Background(), SearchInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID, Query: "没人答过的问题",
	})
	require.NoError(t, err)
	require.Equal(t, value.RetrievalStatusEmpty, response.Run.RetrievalStatus)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	recorder.Close(ctx)

	citations, logs, _ := store.snapshot()
	require.Empty(t, citations, "empty run 无结果行，不投引用")
	require.Len(t, logs, 1, "empty run 的原文是零召回洞察的核心输入")
	require.Equal(t, value.RetrievalStatusEmpty, logs[0].RetrievalStatus)
}

func TestBuildCitationEntriesRules(t *testing.T) {
	workspaceID, kbID, runID := uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC()

	multi := &dto.SearchResult{
		KnowledgeBaseID: kbID, DocumentID: uuid.New(), ChunkRevisionID: uuid.New(),
	}
	other := &dto.SearchResult{
		KnowledgeBaseID: uuid.New(), DocumentID: uuid.New(), ChunkRevisionID: uuid.New(),
	}

	t.Run("多库行按自带 KBID 归属且 rank 连续", func(t *testing.T) {
		entries := buildCitationEntries(workspaceID, uuid.Nil, runID, []*dto.SearchResult{multi, other}, value.RetrievalStatusAvailable, now)
		require.Len(t, entries, 2)
		require.Equal(t, kbID, entries[0].KbID)
		require.Equal(t, other.KnowledgeBaseID, entries[1].KbID)
		require.Equal(t, 1, entries[0].Rank)
		require.Equal(t, 2, entries[1].Rank)
	})

	t.Run("单库 fallback KbID 生效", func(t *testing.T) {
		single := &dto.SearchResult{DocumentID: uuid.New(), ChunkRevisionID: uuid.New()}
		entries := buildCitationEntries(workspaceID, kbID, runID, []*dto.SearchResult{single}, value.RetrievalStatusDegraded, now)
		require.Len(t, entries, 1)
		require.Equal(t, kbID, entries[0].KbID)
		require.Equal(t, value.RetrievalStatusDegraded, entries[0].RetrievalStatus)
	})

	t.Run("empty/failed 与空结果不产出", func(t *testing.T) {
		require.Empty(t, buildCitationEntries(workspaceID, kbID, runID, nil, value.RetrievalStatusEmpty, now))
		require.Empty(t, buildCitationEntries(workspaceID, kbID, runID, []*dto.SearchResult{multi}, value.RetrievalStatusEmpty, now))
	})

	t.Run("缺 lineage 的行被跳过", func(t *testing.T) {
		broken := &dto.SearchResult{KnowledgeBaseID: kbID, DocumentID: uuid.Nil, ChunkRevisionID: uuid.New()}
		entries := buildCitationEntries(workspaceID, kbID, runID, []*dto.SearchResult{broken, multi}, value.RetrievalStatusAvailable, now)
		require.Len(t, entries, 1)
	})
}

func TestQueryLogEnabledDegradesOnFlagReadFailure(t *testing.T) {
	flags := &kbFlagsStub{err: errors.New("boom")}
	require.False(t, queryLogEnabled(context.Background(), nil, flags, uuid.New(), uuid.New()), "读取失败必须降级为不记明文")
	require.False(t, queryLogEnabled(context.Background(), nil, nil, uuid.New(), uuid.New()), "flags 未装配时不记明文")
	kbID := uuid.New()
	enabled := &kbFlagsStub{enabled: map[uuid.UUID]bool{kbID: true}}
	require.True(t, queryLogEnabled(context.Background(), nil, enabled, uuid.New(), kbID))
}
