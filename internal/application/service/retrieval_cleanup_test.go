package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

func TestRetrievalCleanupBuildsWorkspaceScopedRetentionRequest(t *testing.T) {
	now := time.Date(2026, 7, 31, 10, 30, 0, 0, time.UTC)
	workspaceID := uuid.New()
	store := &fakeRetrievalCleanupStore{result: RetrievalCleanupResult{
		DeletedEntries: 7, DeletedGenerations: 2,
	}}
	cleanup := NewRetrievalCleanupService(store, RetrievalCleanupOptions{
		FailedStagingRetention:     24 * time.Hour,
		RetiredGenerationRetention: 7 * 24 * time.Hour,
		BatchSize:                  10,
	})
	cleanup.now = func() time.Time { return now }

	got, err := cleanup.Cleanup(context.Background(), workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if got != store.result {
		t.Fatalf("result = %#v, want %#v", got, store.result)
	}
	if store.request.WorkspaceID != workspaceID || store.request.BatchSize != 10 {
		t.Fatalf("request scope = %#v", store.request)
	}
	if want := now.Add(-24 * time.Hour); !store.request.FailedStagingBefore.Equal(want) {
		t.Fatalf("failed cutoff = %v, want %v", store.request.FailedStagingBefore, want)
	}
	if want := now.Add(-7 * 24 * time.Hour); !store.request.RetiredBefore.Equal(want) {
		t.Fatalf("retired cutoff = %v, want %v", store.request.RetiredBefore, want)
	}
}

func TestRetrievalCleanupRejectsInvalidScopeAndPolicyBeforeStore(t *testing.T) {
	tests := []struct {
		name      string
		workspace uuid.UUID
		options   RetrievalCleanupOptions
	}{
		{
			name: "empty workspace", workspace: uuid.Nil,
			options: RetrievalCleanupOptions{FailedStagingRetention: time.Hour, RetiredGenerationRetention: time.Hour, BatchSize: 1},
		},
		{
			name: "invalid failed retention", workspace: uuid.New(),
			options: RetrievalCleanupOptions{RetiredGenerationRetention: time.Hour, BatchSize: 1},
		},
		{
			name: "invalid retired retention", workspace: uuid.New(),
			options: RetrievalCleanupOptions{FailedStagingRetention: time.Hour, BatchSize: 1},
		},
		{
			name: "invalid batch", workspace: uuid.New(),
			options: RetrievalCleanupOptions{FailedStagingRetention: time.Hour, RetiredGenerationRetention: time.Hour, BatchSize: 10001},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeRetrievalCleanupStore{}
			_, err := NewRetrievalCleanupService(store, test.options).Cleanup(context.Background(), test.workspace)
			if !errors.Is(err, domainerrors.ErrValidation) {
				t.Fatalf("error = %v, want validation", err)
			}
			if store.calls != 0 {
				t.Fatalf("store calls = %d, want 0", store.calls)
			}
		})
	}
}

type fakeRetrievalCleanupStore struct {
	request RetrievalCleanupRequest
	result  RetrievalCleanupResult
	err     error
	calls   int
}

func (s *fakeRetrievalCleanupStore) Cleanup(_ context.Context, request RetrievalCleanupRequest) (RetrievalCleanupResult, error) {
	s.calls++
	s.request = request
	return s.result, s.err
}
