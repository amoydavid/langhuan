//go:build integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/dajee/langhuan/internal/domain/model"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func newSearchRunRepositoryHarness(t *testing.T) (*SearchRunRepository, knowledgeSchemaSeed, knowledgeSchemaSeed) {
	t.Helper()
	ctx, database := openIntegrationTestDB(t)
	var seedA, seedB knowledgeSchemaSeed
	require.NoError(t, database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		seedA = insertKnowledgeSchemaSeed(t, ctx, tx)
		seedB = insertKnowledgeSchemaSeed(t, ctx, tx)
		return nil
	}))
	repo := NewSearchRunRepository(database)
	return repo, seedA, seedB
}

func readyRunningSearchRun(seed knowledgeSchemaSeed, now time.Time) *model.SearchRun {
	return &model.SearchRun{
		ID:             uuid.New(),
		WorkspaceID:    seed.workspaceID,
		RequestedScope: value.SearchScopeSelected,
		QueryHash:      "sha256:v1:abc",
		QueryChars:     4,
		VectorTopK:     20,
		KeywordTopK:    20,
		FinalTopK:      10,
		RetrievalStatus: value.RetrievalStatusRunning,
		CreatedAt:      now,
		ExpiresAt:      now.Add(168 * time.Hour),
	}
}

func searchRunGenerationFor(seed knowledgeSchemaSeed, runID uuid.UUID) model.SearchRunGeneration {
	return model.SearchRunGeneration{
		ID: uuid.New(), WorkspaceID: seed.workspaceID, SearchRunID: runID,
		KnowledgeBaseID: seed.kbID, GenerationID: seed.generationID,
		SourceContentVersion: 1, IndexedContentVersion: 1,
		GenerationConfigHash: "config-hash", EmbeddingModelID: seed.modelID,
		ProviderID: seed.providerID, ModelName: "text-embedding",
		ModelConfigHash: "model-hash", EmbeddingDimension: 1024,
		RetrievalConfigHash: "retrieval-hash",
	}
}

func TestSearchRunRepositoryLifecycleAndIsolation(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	repo, seedA, seedB := newSearchRunRepositoryHarness(t)

	run := readyRunningSearchRun(seedA, now)
	require.NoError(t, repo.Create(ctx, run))

	require.NoError(t, repo.Complete(ctx, seedA.workspaceID, run.ID, model.SearchRunCompletion{
		Status:       value.RetrievalStatusAvailable,
		RankingStage: value.RankingStageRRF,
		ResultCount:  2,
		Generations:  []model.SearchRunGeneration{searchRunGenerationFor(seedA, run.ID)},
	}))

	got, err := repo.Get(ctx, seedA.workspaceID, run.ID)
	require.NoError(t, err)
	require.Equal(t, value.RetrievalStatusAvailable, got.RetrievalStatus)
	require.Equal(t, 2, got.ResultCount)
	require.Len(t, got.Generations, 1)
	require.NotEmpty(t, got.CompletedAt)

	// 跨 Workspace 读取被拒绝。
	_, err = repo.Get(ctx, seedB.workspaceID, run.ID)
	require.ErrorIs(t, err, domainerrors.ErrNotFound)
}

func TestSearchRunRepositoryCompleteRejectsAlreadyTerminal(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	repo, seedA, _ := newSearchRunRepositoryHarness(t)

	run := readyRunningSearchRun(seedA, now)
	require.NoError(t, repo.Create(ctx, run))
	require.NoError(t, repo.Complete(ctx, seedA.workspaceID, run.ID, model.SearchRunCompletion{
		Status: value.RetrievalStatusEmpty, RankingStage: value.RankingStageRRF,
	}))

	err := repo.Complete(ctx, seedA.workspaceID, run.ID, model.SearchRunCompletion{
		Status: value.RetrievalStatusAvailable, RankingStage: value.RankingStageRRF, ResultCount: 1,
	})
	require.ErrorIs(t, err, domainerrors.ErrConflict)
}

func TestSearchRunRepositoryCompleteRejectsFailedWithoutFailureClass(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	repo, seedA, _ := newSearchRunRepositoryHarness(t)

	run := readyRunningSearchRun(seedA, now)
	require.NoError(t, repo.Create(ctx, run))

	err := repo.Complete(ctx, seedA.workspaceID, run.ID, model.SearchRunCompletion{
		Status: value.RetrievalStatusFailed,
	})
	require.ErrorIs(t, err, domainerrors.ErrValidation)
}

func TestSearchRunRepositoryDeleteExpiredOnlyDeletesPastExpiresAt(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	repo, seedA, _ := newSearchRunRepositoryHarness(t)

	expired := readyRunningSearchRun(seedA, now)
	expired.ExpiresAt = now.Add(-1 * time.Hour)
	require.NoError(t, repo.Create(ctx, expired))

	fresh := readyRunningSearchRun(seedA, now)
	require.NoError(t, repo.Create(ctx, fresh))

	deleted, err := repo.DeleteExpired(ctx, now, 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	_, err = repo.Get(ctx, seedA.workspaceID, expired.ID)
	require.ErrorIs(t, err, domainerrors.ErrNotFound)
	_, err = repo.Get(ctx, seedA.workspaceID, fresh.ID)
	require.NoError(t, err)
}

func TestSearchRunRepositoryRejectsCrossWorkspaceGenerationSnapshot(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	repo, seedA, seedB := newSearchRunRepositoryHarness(t)

	run := readyRunningSearchRun(seedA, now)
	require.NoError(t, repo.Create(ctx, run))

	// 使用 seedB 的 generation（跨 Workspace）应该被 FK 拒绝。
	crossGen := model.SearchRunGeneration{
		ID: uuid.New(), WorkspaceID: seedA.workspaceID, SearchRunID: run.ID,
		KnowledgeBaseID: seedB.kbID, GenerationID: seedB.generationID,
		SourceContentVersion: 1, IndexedContentVersion: 1,
		GenerationConfigHash: "config-hash", EmbeddingModelID: seedA.modelID,
		ProviderID: seedA.providerID, ModelName: "text-embedding",
		ModelConfigHash: "model-hash", EmbeddingDimension: 1024,
		RetrievalConfigHash: "retrieval-hash",
	}
	err := repo.Complete(ctx, seedA.workspaceID, run.ID, model.SearchRunCompletion{
		Status: value.RetrievalStatusAvailable, RankingStage: value.RankingStageRRF,
		ResultCount: 1, Generations: []model.SearchRunGeneration{crossGen},
	})
	require.Error(t, err, "cross-workspace generation snapshot should be rejected")
}

func TestSearchRunRepositoryReplayOfIDMustSameWorkspace(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	repo, seedA, seedB := newSearchRunRepositoryHarness(t)

	// 在 seedB 创建一个 run，然后尝试在 seedA 用它作为 replay_of_id。
	otherRun := readyRunningSearchRun(seedB, now)
	require.NoError(t, repo.Create(ctx, otherRun))

	run := readyRunningSearchRun(seedA, now)
	run.ReplayOfID = &otherRun.ID
	err := repo.Create(ctx, run)
	require.Error(t, err, "cross-workspace replay_of_id should be rejected")
}

func TestSearchRunRepositoryGenerationCascadeDelete(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	_, database := openIntegrationTestDB(t)
	var seedA knowledgeSchemaSeed
	require.NoError(t, database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		seedA = insertKnowledgeSchemaSeed(t, ctx, tx)
		return nil
	}))
	repo := NewSearchRunRepository(database)

	run := readyRunningSearchRun(seedA, now)
	require.NoError(t, repo.Create(ctx, run))
	require.NoError(t, repo.Complete(ctx, seedA.workspaceID, run.ID, model.SearchRunCompletion{
		Status:       value.RetrievalStatusAvailable,
		RankingStage: value.RankingStageRRF,
		ResultCount:  1,
		Generations:  []model.SearchRunGeneration{searchRunGenerationFor(seedA, run.ID)},
	}))

	var count int64
	require.NoError(t, database.WithContext(ctx).Table("search_run_generations").
		Where("search_run_id = ?", run.ID).Count(&count).Error)
	require.EqualValues(t, 1, count)

	require.NoError(t, database.WithContext(ctx).Where("id = ?", run.ID).
		Delete(&SearchRunRow{}).Error)

	require.NoError(t, database.WithContext(ctx).Table("search_run_generations").
		Where("search_run_id = ?", run.ID).Count(&count).Error)
	require.EqualValues(t, 0, count)
}
