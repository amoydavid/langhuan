package db

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestWorkspaceSearchSettingsCodecRoundTrip(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Microsecond)
	want := &model.WorkspaceSearchSettings{
		WorkspaceID: uuid.New(), UpdatedBy: uuid.New(), CreatedAt: now, UpdatedAt: now,
		Rerank: &model.RerankSnapshot{
			ModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "BAAI/bge-reranker-v2-m3",
			ModelConfigHash: "config-hash", CandidateTopK: 80, FailureMode: value.RerankFailureFail,
		},
	}
	row, err := workspaceSearchSettingsToRow(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := workspaceSearchSettingsFromRow(row)
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkspaceID != want.WorkspaceID || got.UpdatedBy != want.UpdatedBy || got.Rerank == nil ||
		got.Rerank.ModelID != want.Rerank.ModelID || got.Rerank.ProviderID != want.Rerank.ProviderID ||
		got.Rerank.ModelName != want.Rerank.ModelName || got.Rerank.ModelConfigHash != want.Rerank.ModelConfigHash ||
		got.Rerank.CandidateTopK != want.Rerank.CandidateTopK || got.Rerank.FailureMode != want.Rerank.FailureMode {
		t.Fatalf("round trip mismatch\nwant=%#v\n got=%#v", want, got)
	}
}

func TestWorkspaceSearchSettingsCodecDisabled(t *testing.T) {
	t.Parallel()
	want := &model.WorkspaceSearchSettings{WorkspaceID: uuid.New(), UpdatedBy: uuid.New()}
	row, err := workspaceSearchSettingsToRow(want)
	if err != nil {
		t.Fatal(err)
	}
	if row.RerankModelID != nil || len(row.RerankConfig) != 0 {
		t.Fatalf("disabled row = %#v", row)
	}
	got, err := workspaceSearchSettingsFromRow(row)
	if err != nil || got.Rerank != nil {
		t.Fatalf("disabled round trip = %#v err=%v", got, err)
	}
}
