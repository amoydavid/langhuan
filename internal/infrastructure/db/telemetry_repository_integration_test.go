//go:build integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func newTelemetryRepositoryHarness(t *testing.T) (*TelemetryRepository, knowledgeSchemaSeed, knowledgeSchemaSeed) {
	t.Helper()
	ctx, database := openIntegrationTestDB(t)
	var seedA, seedB knowledgeSchemaSeed
	require.NoError(t, database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		seedA = insertKnowledgeSchemaSeed(t, ctx, tx)
		seedB = insertKnowledgeSchemaSeed(t, ctx, tx)
		return nil
	}))
	return NewTelemetryRepository(database), seedA, seedB
}

func validCitationFor(seed knowledgeSchemaSeed, runID uuid.UUID, rank int) model.SearchCitation {
	return model.SearchCitation{
		ID: uuid.New(), WorkspaceID: seed.workspaceID, KbID: seed.kbID,
		SearchRunID: runID, DocumentID: uuid.New(), ChunkRevisionID: uuid.New(),
		Rank: rank, RetrievalStatus: value.RetrievalStatusAvailable,
	}
}

func TestTelemetryRepositoryInsertAndConflictDowngrade(t *testing.T) {
	ctx := context.Background()
	repo, seedA, _ := newTelemetryRepositoryHarness(t)
	runID := uuid.New()

	entries := []model.SearchCitation{
		validCitationFor(seedA, runID, 1),
		validCitationFor(seedA, runID, 2),
	}
	require.NoError(t, repo.InsertCitations(ctx, entries))

	// 冲突降级：同 run 同 chunk_revision 重复投递整批不报错，且新增行落库。
	dup := entries[0]
	dup.ID = uuid.New()
	extra := validCitationFor(seedA, runID, 3)
	require.NoError(t, repo.InsertCitations(ctx, []model.SearchCitation{dup, extra}))

	var count int64
	require.NoError(t, repo.db.WithContext(ctx).Model(&SearchCitationRow{}).
		Where("search_run_id = ?", runID).Count(&count).Error)
	require.Equal(t, int64(3), count, "冲突行跳过、新行保留")
}

func TestTelemetryRepositoryInsertValidation(t *testing.T) {
	ctx := context.Background()
	repo, seedA, _ := newTelemetryRepositoryHarness(t)

	badRank := validCitationFor(seedA, uuid.New(), 0)
	require.ErrorIs(t, repo.InsertCitations(ctx, []model.SearchCitation{badRank}), domainerrors.ErrValidation)

	badStatus := validCitationFor(seedA, uuid.New(), 1)
	badStatus.RetrievalStatus = value.RetrievalStatusEmpty
	require.ErrorIs(t, repo.InsertCitations(ctx, []model.SearchCitation{badStatus}), domainerrors.ErrValidation)

	blankText := model.SearchQueryLogEntry{
		ID: uuid.New(), WorkspaceID: seedA.workspaceID, KbID: seedA.kbID,
		SearchRunID: uuid.New(), QueryText: "  ", QueryHash: "h", QueryChars: 2,
		RetrievalStatus: value.RetrievalStatusEmpty,
	}
	require.ErrorIs(t, repo.InsertQueryLogEntries(ctx, []model.SearchQueryLogEntry{blankText}), domainerrors.ErrValidation)

	require.NoError(t, repo.InsertCitations(ctx, nil))
	require.NoError(t, repo.InsertQueryLogEntries(ctx, nil))
}

func TestTelemetryRepositoryQueryLogEnabledRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo, seedA, seedB := newTelemetryRepositoryHarness(t)

	enabled, err := repo.GetQueryLogEnabled(ctx, seedA.workspaceID, seedA.kbID)
	require.NoError(t, err)
	require.False(t, enabled, "默认关闭")

	require.NoError(t, repo.UpdateQueryLogEnabled(ctx, seedA.workspaceID, seedA.kbID, true))
	enabled, err = repo.GetQueryLogEnabled(ctx, seedA.workspaceID, seedA.kbID)
	require.NoError(t, err)
	require.True(t, enabled)

	// 跨 workspace / 不存在的 KB 返回 ErrNotFound。
	_, err = repo.GetQueryLogEnabled(ctx, seedB.workspaceID, seedA.kbID)
	require.ErrorIs(t, err, domainerrors.ErrNotFound)
	err = repo.UpdateQueryLogEnabled(ctx, seedA.workspaceID, uuid.New(), true)
	require.ErrorIs(t, err, domainerrors.ErrNotFound)
}

func TestTelemetryRepositoryDeleteExpiredRespectsRetention(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	repo, seedA, _ := newTelemetryRepositoryHarness(t)

	oldRun, freshRun := uuid.New(), uuid.New()
	old := validCitationFor(seedA, oldRun, 1)
	old.CreatedAt = now.Add(-40 * 24 * time.Hour)
	fresh := validCitationFor(seedA, freshRun, 1)
	require.NoError(t, repo.InsertCitations(ctx, []model.SearchCitation{old, fresh}))

	oldLog := model.SearchQueryLogEntry{
		ID: uuid.New(), WorkspaceID: seedA.workspaceID, KbID: seedA.kbID,
		SearchRunID: uuid.New(), QueryText: "旧问题", QueryHash: "h1", QueryChars: 3,
		RetrievalStatus: value.RetrievalStatusEmpty, CreatedAt: now.Add(-40 * 24 * time.Hour),
	}
	freshLog := model.SearchQueryLogEntry{
		ID: uuid.New(), WorkspaceID: seedA.workspaceID, KbID: seedA.kbID,
		SearchRunID: uuid.New(), QueryText: "新问题", QueryHash: "h2", QueryChars: 3,
		RetrievalStatus: value.RetrievalStatusEmpty, CreatedAt: now,
	}
	require.NoError(t, repo.InsertQueryLogEntries(ctx, []model.SearchQueryLogEntry{oldLog, freshLog}))

	cutoff := now.Add(-35 * 24 * time.Hour)
	deleted, err := repo.DeleteExpiredCitations(ctx, cutoff, 100)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)
	deleted, err = repo.DeleteExpiredQueryLogs(ctx, cutoff, 100)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)

	var citationCount, logCount int64
	require.NoError(t, repo.db.WithContext(ctx).Model(&SearchCitationRow{}).Count(&citationCount).Error)
	require.NoError(t, repo.db.WithContext(ctx).Model(&SearchQueryLogRow{}).Count(&logCount).Error)
	require.Equal(t, int64(1), citationCount)
	require.Equal(t, int64(1), logCount)

	// 清理幂等：再次执行删除 0 行。
	deleted, err = repo.DeleteExpiredCitations(ctx, cutoff, 100)
	require.NoError(t, err)
	require.Equal(t, int64(0), deleted)
}
