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
