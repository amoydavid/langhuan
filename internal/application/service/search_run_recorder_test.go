package service

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

type fakeSearchRunStore struct {
	createErr     error
	completeErr   error
	created       *model.SearchRun
	completions   []recordedCompletion
	getRun        *model.SearchRun
	deleted       int64
	deleteErr     error
	limit         int
}

type recordedCompletion struct {
	workspaceID uuid.UUID
	runID       uuid.UUID
	completion  model.SearchRunCompletion
}

func (s *fakeSearchRunStore) Create(_ context.Context, run *model.SearchRun) error {
	s.created = run
	return s.createErr
}
func (s *fakeSearchRunStore) Complete(_ context.Context, workspaceID, runID uuid.UUID, completion model.SearchRunCompletion) error {
	s.completions = append(s.completions, recordedCompletion{workspaceID, runID, completion})
	return s.completeErr
}
func (s *fakeSearchRunStore) Get(_ context.Context, _, _ uuid.UUID) (*model.SearchRun, error) {
	return s.getRun, nil
}
func (s *fakeSearchRunStore) DeleteExpired(_ context.Context, _ time.Time, limit int) (int64, error) {
	s.limit = limit
	return s.deleted, s.deleteErr
}

func TestRecorderPersistenceFailureDoesNotReplaceOutcome(t *testing.T) {
	store := &fakeSearchRunStore{completeErr: errors.New("db unavailable")}
	now := time.Now()
	recorder := newSearchRunRecorder(
		store, slog.Default(), func() time.Time { return now }, 168*time.Hour,
		uuid.New(), "sha256:v1:abc", 4, 20, 20, 10,
		value.SearchScopeSelected, "http", "req-1", "user", nil,
	)
	recorder.Finish(context.Background(), value.RetrievalStatusAvailable, "", value.RankingStageRRF, 1, nil)
	require.Error(t, recorder.PersistenceError())
}

func TestRecorderCreateFailureRecordsError(t *testing.T) {
	store := &fakeSearchRunStore{createErr: errors.New("db unavailable")}
	now := time.Now()
	recorder := newSearchRunRecorder(
		store, slog.Default(), func() time.Time { return now }, 168*time.Hour,
		uuid.New(), "sha256:v1:abc", 4, 20, 20, 10,
		value.SearchScopeSelected, "http", "req-1", "user", nil,
	)
	require.Error(t, recorder.PersistenceError())
	recorder.Finish(context.Background(), value.RetrievalStatusAvailable, "", value.RankingStageRRF, 1, nil)
	// Create 失败后不应调用 Complete。
	require.Empty(t, store.completions)
}

func TestRecorderCompletesWithTerminalStatus(t *testing.T) {
	store := &fakeSearchRunStore{}
	now := time.Now()
	wsID := uuid.New()
	recorder := newSearchRunRecorder(
		store, slog.Default(), func() time.Time { return now }, 168*time.Hour,
		wsID, "sha256:v1:abc", 4, 20, 20, 10,
		value.SearchScopeSelected, "http", "req-1", "user", nil,
	)
	require.Equal(t, wsID, store.created.WorkspaceID)
	require.Equal(t, value.RetrievalStatusRunning, store.created.RetrievalStatus)

	recorder.Finish(context.Background(), value.RetrievalStatusDegraded, "", value.RankingStageRRFFallback, 3, nil)
	require.Len(t, store.completions, 1)
	require.Equal(t, value.RetrievalStatusDegraded, store.completions[0].completion.Status)
	require.Equal(t, 3, store.completions[0].completion.ResultCount)
	require.NoError(t, recorder.PersistenceError())
}

func TestRetrievalConfigHashStable(t *testing.T) {
	h1 := retrievalConfigHash(map[string]any{"a": 1, "b": "x"})
	h2 := retrievalConfigHash(map[string]any{"b": "x", "a": 1})
	require.Equal(t, h1, h2)
	require.Len(t, h1, 64)
}
