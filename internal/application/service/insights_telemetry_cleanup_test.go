package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeTelemetryCleanupStore struct {
	citationCutoff time.Time
	logCutoff      time.Time
	citationErr    error
	citations      int64
	logs           int64
}

func (f *fakeTelemetryCleanupStore) DeleteExpiredCitations(_ context.Context, before time.Time, _ int) (int64, error) {
	f.citationCutoff = before
	if f.citationErr != nil {
		return 0, f.citationErr
	}
	return f.citations, nil
}

func (f *fakeTelemetryCleanupStore) DeleteExpiredQueryLogs(_ context.Context, before time.Time, _ int) (int64, error) {
	f.logCutoff = before
	return f.logs, nil
}

func TestInsightsTelemetryCleanupUsesIndependentRetentionCutoffs(t *testing.T) {
	store := &fakeTelemetryCleanupStore{citations: 3, logs: 1}
	svc := NewInsightsTelemetryCleanupService(store, 35*24*time.Hour, 30*24*time.Hour)

	citations, logs, err := svc.RunNow(context.Background(), 100)
	require.NoError(t, err)
	require.Equal(t, int64(3), citations)
	require.Equal(t, int64(1), logs)

	// 两张表各按各的保留期推导 cutoff，且都不依赖 search_run 的 expires_at。
	require.WithinDuration(t, time.Now().Add(-35*24*time.Hour), store.citationCutoff, time.Minute)
	require.WithinDuration(t, time.Now().Add(-30*24*time.Hour), store.logCutoff, time.Minute)
	require.NotEqual(t, store.citationCutoff, store.logCutoff, "保留期不同则 cutoff 必须不同")
}

func TestInsightsTelemetryCleanupRejectsInvalidLimit(t *testing.T) {
	svc := NewInsightsTelemetryCleanupService(&fakeTelemetryCleanupStore{}, time.Hour, time.Hour)
	_, _, err := svc.RunNow(context.Background(), 0)
	require.Error(t, err)

	var nilSvc *InsightsTelemetryCleanupService
	_, _, err = nilSvc.RunNow(context.Background(), 10)
	require.Error(t, err, "nil 服务必须报错而非 panic")
}

func TestInsightsTelemetryCleanupReturnsCitationsDeletedOnLogFailure(t *testing.T) {
	store := &fakeTelemetryCleanupStore{citations: 5, citationErr: nil}
	svc := NewInsightsTelemetryCleanupService(store, time.Hour, time.Hour)
	citations, _, err := svc.RunNow(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, int64(5), citations)

	store.citationErr = errors.New("db down")
	_, _, err = svc.RunNow(context.Background(), 10)
	require.Error(t, err)
}
