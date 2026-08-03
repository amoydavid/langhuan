package model

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestNewKnowledgeBaseRequiresWorkspaceID(t *testing.T) {
	if _, err := NewKnowledgeBase(uuid.Nil, "kb", "", uuid.New(), nil, nil); !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("expected validation error for workspace id, got %v", err)
	}
}

func TestNewKnowledgeBaseValidatesInput(t *testing.T) {
	workspaceID := uuid.New()

	if _, err := NewKnowledgeBase(workspaceID, "", "desc", uuid.New(), nil, nil); !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("expected validation error for name, got %v", err)
	}
	if _, err := NewKnowledgeBase(workspaceID, "kb", "desc", uuid.Nil, nil, nil); !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("expected validation error for model id, got %v", err)
	}
}

func TestNewKnowledgeBaseAppliesDefaultChunkingConfig(t *testing.T) {
	kb, err := NewKnowledgeBase(uuid.New(), "kb", "desc", uuid.New(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if kb.ChunkingConfig.ChunkSize != 512 {
		t.Fatalf("chunk size = %d", kb.ChunkingConfig.ChunkSize)
	}
	if kb.ChunkingConfig.ChunkOverlap != 80 {
		t.Fatalf("chunk overlap = %d", kb.ChunkingConfig.ChunkOverlap)
	}
}

func TestNewKnowledgeBaseDefaultsMetadataToEmptyMap(t *testing.T) {
	kb, err := NewKnowledgeBase(uuid.New(), "kb", "desc", uuid.New(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if kb.Metadata == nil {
		t.Fatal("metadata should default to an empty map")
	}
	if len(kb.Metadata) != 0 {
		t.Fatalf("metadata = %#v", kb.Metadata)
	}
}

func TestNewKnowledgeBaseKeepsExplicitZeroOverlap(t *testing.T) {
	chunking := &value.ChunkingConfig{ChunkSize: 32, ChunkOverlap: 0}
	kb, err := NewKnowledgeBase(uuid.New(), "kb", "desc", uuid.New(), chunking, nil)
	if err != nil {
		t.Fatal(err)
	}
	if kb.ChunkingConfig.ChunkOverlap != 0 {
		t.Fatalf("chunk overlap = %d, want 0", kb.ChunkingConfig.ChunkOverlap)
	}
}

func TestNewKnowledgeBaseRejectsOverlapNotSmallerThanSize(t *testing.T) {
	chunking := &value.ChunkingConfig{ChunkSize: 32, ChunkOverlap: 32}
	if _, err := NewKnowledgeBase(uuid.New(), "kb", "desc", uuid.New(), chunking, nil); !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("error = %v, want validation", err)
	}
}
