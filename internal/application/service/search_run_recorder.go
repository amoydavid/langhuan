package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// searchRunRecorder 管理一次检索运行的 SearchRun 生命周期。
// Create/Complete 失败只记录 search_run_persistence_failed，不覆盖搜索 results/error。
type searchRunRecorder struct {
	store            SearchRunStore
	logger           *slog.Logger
	now              func() time.Time
	retention        time.Duration
	workspaceID      uuid.UUID
	runID            uuid.UUID
	createdAt        time.Time
	queryHash        string
	queryChars       int
	vectorTopK       int
	keywordTopK      int
	finalTopK        int
	requestedScope   value.SearchScope
	transport        string
	requestID        string
	principalKind    string
	replayOfID       *uuid.UUID
	createFailed     bool
	persistenceError error
}

func newSearchRunRecorder(
	store SearchRunStore,
	logger *slog.Logger,
	now func() time.Time,
	retention time.Duration,
	workspaceID uuid.UUID,
	queryHash string,
	queryChars, vectorTopK, keywordTopK, finalTopK int,
	requestedScope value.SearchScope,
	transport, requestID, principalKind string,
	replayOfID *uuid.UUID,
) *searchRunRecorder {
	runID := uuid.New()
	current := now()
	recorder := &searchRunRecorder{
		store:          store,
		logger:         logger,
		now:            now,
		retention:      retention,
		workspaceID:    workspaceID,
		runID:          runID,
		createdAt:      current,
		queryHash:      queryHash,
		queryChars:     queryChars,
		vectorTopK:     vectorTopK,
		keywordTopK:    keywordTopK,
		finalTopK:      finalTopK,
		requestedScope: requestedScope,
		transport:      transport,
		requestID:      requestID,
		principalKind:  principalKind,
		replayOfID:     replayOfID,
	}
	if store != nil {
		run := &model.SearchRun{
			ID:              runID,
			WorkspaceID:     workspaceID,
			RequestedScope:  requestedScope,
			QueryHash:       queryHash,
			QueryChars:      queryChars,
			VectorTopK:      vectorTopK,
			KeywordTopK:     keywordTopK,
			FinalTopK:       finalTopK,
			RetrievalStatus: value.RetrievalStatusRunning,
			CreatedAt:       current,
			ExpiresAt:       current.Add(retention),
			ReplayOfID:      replayOfID,
			Transport:       transport,
			RequestID:       requestID,
			PrincipalKind:   principalKind,
		}
		if err := store.Create(context.Background(), run); err != nil {
			recorder.createFailed = true
			recorder.persistenceError = err
			logger.Warn("search_run_persistence_failed",
				slog.String("event", "search_run_persistence_failed"),
				slog.String("phase", "create"),
				slog.String("error", err.Error()),
			)
		}
	}
	return recorder
}

// AddGeneration 追加一个 Generation 快照到终态完成时使用。
func (r *searchRunRecorder) generationSnapshot(gen *model.IndexGeneration) model.SearchRunGeneration {
	return model.SearchRunGeneration{
		ID: uuid.New(), WorkspaceID: r.workspaceID, SearchRunID: r.runID,
		KnowledgeBaseID: gen.KnowledgeBaseID, GenerationID: gen.ID,
		SourceContentVersion:  gen.SourceContentVersion,
		IndexedContentVersion: gen.IndexedContentVersion,
		GenerationConfigHash:  gen.ConfigHash,
		EmbeddingModelID:      gen.EmbeddingModelID,
		ProviderID:            gen.ProviderID,
		ModelName:             gen.ModelName,
		ModelConfigHash:       gen.ModelConfigHash,
		EmbeddingDimension:    gen.EmbeddingDimension,
		RetrievalConfigHash:   retrievalConfigHash(gen.RetrievalConfig),
		RerankSnapshot:        nil,
	}
}

// Finish 完成 SearchRun 终态。status、failureClass、rankingStage、resultCount 决定终态；
// generations 是参与的 Generation 快照。Create/Complete 失败只记录，不返回错误。
func (r *searchRunRecorder) Finish(ctx context.Context, status value.RetrievalStatus, failureClass string, rankingStage value.RankingStage, resultCount int, generations []model.SearchRunGeneration) {
	if r == nil || r.store == nil || r.createFailed {
		return
	}
	completion := model.SearchRunCompletion{
		Status:       status,
		FailureClass: failureClass,
		RankingStage: rankingStage,
		ResultCount:  resultCount,
		Generations:  generations,
	}
	if err := r.store.Complete(ctx, r.workspaceID, r.runID, completion); err != nil {
		r.persistenceError = err
		r.logger.Warn("search_run_persistence_failed",
			slog.String("event", "search_run_persistence_failed"),
			slog.String("phase", "complete"),
			slog.String("error", err.Error()),
		)
	}
}

// PersistenceError 返回创建或完成阶段的持久化错误（不影响搜索结果）。
func (r *searchRunRecorder) PersistenceError() error {
	if r == nil {
		return nil
	}
	return r.persistenceError
}

// RunID 返回 SearchRun ID。
func (r *searchRunRecorder) RunID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	return r.runID
}

// buildSummary 构造 dto.SearchRunSummary。
func (r *searchRunRecorder) buildSummary(status value.RetrievalStatus, failureClass string, rankingStage value.RankingStage, resultCount int, effectiveKBIDs []uuid.UUID, generationSnapshots []model.SearchRunGeneration, effectiveScope value.SearchScope) dto.SearchRunSummary {
	if r == nil {
		return dto.SearchRunSummary{}
	}
	snapshots := make([]dto.GenerationSnapshot, 0, len(generationSnapshots))
	for _, gen := range generationSnapshots {
		snapshots = append(snapshots, dto.GenerationSnapshot{
			KnowledgeBaseID: gen.KnowledgeBaseID, GenerationID: gen.GenerationID,
			SourceContentVersion: gen.SourceContentVersion, IndexedContentVersion: gen.IndexedContentVersion,
			GenerationConfigHash: gen.GenerationConfigHash, EmbeddingModelID: gen.EmbeddingModelID,
			ProviderID: gen.ProviderID, ModelName: gen.ModelName, ModelConfigHash: gen.ModelConfigHash,
			EmbeddingDimension: gen.EmbeddingDimension, RetrievalConfigHash: gen.RetrievalConfigHash,
			RerankSnapshot: gen.RerankSnapshot,
		})
	}
	return dto.SearchRunSummary{
		SearchID:                  r.runID,
		WorkspaceID:               r.workspaceID,
		RequestedScope:            r.requestedScope,
		EffectiveScope:            effectiveScope,
		EffectiveKnowledgeBaseIDs: effectiveKBIDs,
		GenerationSnapshots:       snapshots,
		QueryHash:                 r.queryHash,
		QueryChars:                r.queryChars,
		VectorTopK:                r.vectorTopK,
		KeywordTopK:               r.keywordTopK,
		FinalTopK:                 r.finalTopK,
		RetrievalStatus:           status,
		FailureClass:              failureClass,
		RankingStage:              rankingStage,
		ResultCount:               resultCount,
		CreatedAt:                 r.createdAt,
		ReplayOfID:                r.replayOfID,
	}
}

// retrievalConfigHash 对 RetrievalConfig map 计算稳定哈希，用于回放验证。
func retrievalConfigHash(config map[string]any) string {
	if config == nil {
		return ""
	}
	keys := make([]string, 0, len(config))
	for k := range config {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	type kv struct {
		Key string `json:"k"`
		Val any    `json:"v"`
	}
	pairs := make([]kv, len(keys))
	for i, k := range keys {
		pairs[i] = kv{Key: k, Val: config[k]}
	}
	data, err := json.Marshal(pairs)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
