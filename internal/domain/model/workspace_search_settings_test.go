package model

import (
	"testing"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/value"
)

func TestWorkspaceSearchSettingsValidate(t *testing.T) {
	t.Parallel()
	valid := &WorkspaceSearchSettings{
		WorkspaceID: uuid.New(),
		Rerank: &RerankSnapshot{
			ModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "rerank",
			ModelConfigHash: "hash", CandidateTopK: 50, FailureMode: value.RerankFailureFallback,
		},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}
	if err := (&WorkspaceSearchSettings{WorkspaceID: uuid.New()}).Validate(); err != nil {
		t.Fatalf("disabled settings rejected: %v", err)
	}
	for name, settings := range map[string]*WorkspaceSearchSettings{
		"missing workspace": {Rerank: valid.Rerank.Clone()},
		"invalid rerank":    {WorkspaceID: uuid.New(), Rerank: &RerankSnapshot{ModelID: uuid.New()}},
	} {
		if err := settings.Validate(); err == nil {
			t.Fatalf("%s unexpectedly accepted", name)
		}
	}
}
