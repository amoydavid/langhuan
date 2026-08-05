package model

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestIndexGenerationValidateActivationRejectsContentChange(t *testing.T) {
	baseID := uuid.New()
	generation := &IndexGeneration{
		ID: uuid.New(), BaseGenerationID: &baseID, SourceContentVersion: 4,
		Status: value.IndexGenerationReady,
	}
	err := generation.ValidateActivation(baseID, 5, false)
	if !errors.Is(err, domainerrors.ErrGenerationStale) {
		t.Fatalf("error = %v, want stale", err)
	}
}

func TestIndexGenerationValidateActivationRequiresManualConfirmation(t *testing.T) {
	baseID := uuid.New()
	generation := &IndexGeneration{
		ID: uuid.New(), BaseGenerationID: &baseID, SourceContentVersion: 4,
		Status: value.IndexGenerationReady, ManualEditDisposition: value.ManualEditPending,
	}
	err := generation.ValidateActivation(baseID, 4, false)
	if !errors.Is(err, domainerrors.ErrManualEditConfirmationRequired) {
		t.Fatalf("error = %v, want confirmation required", err)
	}
}

func TestRerankSnapshotValidation(t *testing.T) {
	t.Parallel()
	valid := &RerankSnapshot{
		ModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "rerank",
		ModelConfigHash: "hash", CandidateTopK: 50,
		FailureMode: value.RerankFailureFallback,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid snapshot error = %v", err)
	}

	tests := []struct {
		name         string
		mutate       func(*RerankSnapshot)
		wantSentinel error
	}{
		{"missing model id", func(s *RerankSnapshot) { s.ModelID = uuid.Nil }, domainerrors.ErrValidation},
		{"missing provider id", func(s *RerankSnapshot) { s.ProviderID = uuid.Nil }, domainerrors.ErrValidation},
		{"empty model name", func(s *RerankSnapshot) { s.ModelName = "  " }, domainerrors.ErrValidation},
		{"empty config hash", func(s *RerankSnapshot) { s.ModelConfigHash = "" }, domainerrors.ErrValidation},
		{"candidate below min", func(s *RerankSnapshot) { s.CandidateTopK = 49 }, domainerrors.ErrValidation},
		{"candidate above max", func(s *RerankSnapshot) { s.CandidateTopK = 201 }, domainerrors.ErrValidation},
		{"invalid failure mode", func(s *RerankSnapshot) { s.FailureMode = "bogus" }, domainerrors.ErrValidation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := *valid
			tt.mutate(&snapshot)
			if err := (&snapshot).Validate(); !errors.Is(err, tt.wantSentinel) {
				t.Fatalf("error = %v, want %v", err, tt.wantSentinel)
			}
		})
	}

	// nil 快照视为关闭 Rerank，校验通过。
	if err := (*RerankSnapshot)(nil).Validate(); err != nil {
		t.Fatalf("nil snapshot error = %v", err)
	}
}

func TestNewIndexGenerationAcceptsRerankSnapshot(t *testing.T) {
	t.Parallel()
	rerank := &RerankSnapshot{
		ModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "rerank",
		ModelConfigHash: "hash", CandidateTopK: 100, FailureMode: value.RerankFailureFail,
	}
	generation, err := NewIndexGeneration(NewIndexGenerationInput{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(),
		EmbeddingModelID: uuid.New(), ProviderID: uuid.New(),
		ModelName: "embed", EmbeddingDimension: 1024, ModelConfigHash: "ehash",
		ChunkerVersion: 1, ConfigHash: "chash", Status: value.IndexGenerationBuilding,
		Rerank: rerank,
	})
	if err != nil {
		t.Fatalf("create generation with rerank error = %v", err)
	}
	if generation.Rerank == nil || generation.Rerank.ModelID != rerank.ModelID {
		t.Fatalf("generation rerank = %#v", generation.Rerank)
	}

	// 非法 rerank 快照在构造时被拒绝。
	invalid := *rerank
	invalid.CandidateTopK = 10
	if _, err := NewIndexGeneration(NewIndexGenerationInput{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(),
		EmbeddingModelID: uuid.New(), ProviderID: uuid.New(),
		ModelName: "embed", EmbeddingDimension: 1024, ModelConfigHash: "ehash",
		ChunkerVersion: 1, ConfigHash: "chash", Status: value.IndexGenerationBuilding,
		Rerank: &invalid,
	}); !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("invalid rerank error = %v", err)
	}
}

func TestNewIndexGenerationRejectsUnsupportedDimension(t *testing.T) {
	_, err := NewIndexGeneration(NewIndexGenerationInput{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(),
		EmbeddingModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "embedding",
		EmbeddingDimension: 1536, ModelConfigHash: "model", ChunkerVersion: 1,
		ChunkingConfig: map[string]any{}, RetrievalConfig: map[string]any{}, ConfigHash: "config",
		Status: value.IndexGenerationBuilding,
	})
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("error = %v, want validation", err)
	}
}
