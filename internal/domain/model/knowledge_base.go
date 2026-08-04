package model

import (
	"fmt"
	id "github.com/dajee/langhuan/internal/domain/id"
	"strings"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

type KnowledgeBase struct {
	ID                      uuid.UUID
	WorkspaceID             uuid.UUID
	EmbeddingModelID        uuid.UUID
	Name                    string
	Description             string
	ChunkingConfig          value.ChunkingConfig
	Metadata                map[string]any
	ContentVersion          int64
	ActiveIndexGenerationID *uuid.UUID
	FileTreeRootID          uuid.UUID
	CreatedAt               time.Time
	UpdatedAt               time.Time
	DeletedAt               *time.Time
}

// ResolvedKnowledgeBase carries a KnowledgeBase and its configured model summary.
type ResolvedKnowledgeBase struct {
	KnowledgeBase   *KnowledgeBase
	EmbeddingModel  *ResolvedModel
	RetrievalConfig map[string]any
}

// NewKnowledgeBase creates a KnowledgeBase with an explicit Embedding model reference.
func NewKnowledgeBase(workspaceID uuid.UUID, name, description string, embeddingModelID uuid.UUID, chunking *value.ChunkingConfig, metadata map[string]any) (*KnowledgeBase, error) {
	if workspaceID == uuid.Nil {
		return nil, fmt.Errorf("%w: workspace_id 不能为空", domainerrors.ErrValidation)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: 知识库名称不能为空", domainerrors.ErrValidation)
	}
	if embeddingModelID == uuid.Nil {
		return nil, fmt.Errorf("%w: embedding_model_id 不能为空", domainerrors.ErrValidation)
	}

	cfg := value.DefaultChunkingConfig()
	if chunking != nil {
		cfg = chunking.Normalize()
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	now := time.Now().UTC()
	return &KnowledgeBase{
		ID: id.New(), WorkspaceID: workspaceID, EmbeddingModelID: embeddingModelID,
		Name: name, Description: description, ChunkingConfig: cfg,
		Metadata: metadata, CreatedAt: now, UpdatedAt: now,
	}, nil
}
