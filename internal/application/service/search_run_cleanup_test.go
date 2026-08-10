package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeSearchRunCleanupStore struct {
	deleted   int64
	limit     int
	deleteErr error
}

func (s *fakeSearchRunCleanupStore) DeleteExpired(_ context.Context, _ time.Time, limit int) (int64, error) {
	s.limit = limit
	return s.deleted, s.deleteErr
}

func TestSearchRunCleanupUsesBatchLimit(t *testing.T) {
	store := &fakeSearchRunCleanupStore{deleted: 23}
	fixedNow := time.Now()
	svc := &SearchRunCleanupService{store: store, now: func() time.Time { return fixedNow }}
	count, err := svc.Run(context.Background(), fixedNow, 1000)
	require.NoError(t, err)
	require.EqualValues(t, 23, count)
	require.Equal(t, 1000, store.limit)
}

func TestSearchRunCleanupRejectsNonPositiveLimit(t *testing.T) {
	store := &fakeSearchRunCleanupStore{}
	svc := NewSearchRunCleanupService(store)
	_, err := svc.Run(context.Background(), time.Now(), 0)
	require.Error(t, err)
}

func TestSearchRunCleanupPropagatesError(t *testing.T) {
	store := &fakeSearchRunCleanupStore{deleteErr: errors.New("db unavailable")}
	svc := NewSearchRunCleanupService(store)
	_, err := svc.Run(context.Background(), time.Now(), 100)
	require.Error(t, err)
}
