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

// NewIndexGenerationInput contains one immutable indexing configuration snapshot.
type NewIndexGenerationInput struct {
	WorkspaceID           uuid.UUID
	KnowledgeBaseID       uuid.UUID
	BaseGenerationID      *uuid.UUID
	EmbeddingModelID      uuid.UUID
	ProviderID            uuid.UUID
	ModelName             string
	EmbeddingDimension    int
	ModelConfigHash       string
	ChunkerVersion        int
	ChunkingConfig        map[string]any
	RetrievalConfig       map[string]any
	ConfigHash            string
	SourceContentVersion  int64
	IndexedContentVersion int64
	Status                value.IndexGenerationStatus
	ManualEditDisposition value.ManualEditDisposition
}

// IndexGeneration stores an immutable model/chunk/retrieval configuration snapshot.
type IndexGeneration struct {
	ID                    uuid.UUID
	WorkspaceID           uuid.UUID
	KnowledgeBaseID       uuid.UUID
	BaseGenerationID      *uuid.UUID
	EmbeddingModelID      uuid.UUID
	ProviderID            uuid.UUID
	ModelName             string
	EmbeddingDimension    int
	ModelConfigHash       string
	ChunkerVersion        int
	ChunkingConfig        map[string]any
	RetrievalConfig       map[string]any
	ConfigHash            string
	SourceContentVersion  int64
	IndexedContentVersion int64
	Status                value.IndexGenerationStatus
	DocumentCount         int64
	ChunkCount            int64
	IndexedCount          int64
	ManualEditCount       int64
	DisabledChunkCount    int64
	ManualEditDisposition value.ManualEditDisposition
	ErrorClass            string
	ErrorMessage          string
	CreatedAt             time.Time
	ReadyAt               *time.Time
	ActivatedAt           *time.Time
	RetiredAt             *time.Time
}

// NewIndexGeneration validates and copies an immutable generation snapshot.
func NewIndexGeneration(input NewIndexGenerationInput) (*IndexGeneration, error) {
	if input.WorkspaceID == uuid.Nil || input.KnowledgeBaseID == uuid.Nil ||
		input.EmbeddingModelID == uuid.Nil || input.ProviderID == uuid.Nil {
		return nil, fmt.Errorf("%w: IndexGeneration lineage/model 不能为空", domainerrors.ErrValidation)
	}
	if !supportedEmbeddingDimension(input.EmbeddingDimension) {
		return nil, fmt.Errorf("%w: embedding_dimension=%d", domainerrors.ErrValidation, input.EmbeddingDimension)
	}
	if input.ChunkerVersion < 1 || input.SourceContentVersion < 0 || input.IndexedContentVersion < 0 {
		return nil, fmt.Errorf("%w: generation version 无效", domainerrors.ErrValidation)
	}
	if strings.TrimSpace(input.ModelName) == "" || strings.TrimSpace(input.ModelConfigHash) == "" || strings.TrimSpace(input.ConfigHash) == "" {
		return nil, fmt.Errorf("%w: generation model/hash 不能为空", domainerrors.ErrValidation)
	}
	if !validIndexGenerationStatus(input.Status) {
		return nil, fmt.Errorf("%w: generation status 无效", domainerrors.ErrValidation)
	}
	disposition := input.ManualEditDisposition
	if disposition == "" {
		disposition = value.ManualEditNotApplicable
	}
	return &IndexGeneration{
		ID: id.New(), WorkspaceID: input.WorkspaceID, KnowledgeBaseID: input.KnowledgeBaseID,
		BaseGenerationID: input.BaseGenerationID, EmbeddingModelID: input.EmbeddingModelID,
		ProviderID: input.ProviderID, ModelName: strings.TrimSpace(input.ModelName),
		EmbeddingDimension: input.EmbeddingDimension, ModelConfigHash: input.ModelConfigHash,
		ChunkerVersion: input.ChunkerVersion, ChunkingConfig: cloneStringAnyMap(input.ChunkingConfig),
		RetrievalConfig: cloneStringAnyMap(input.RetrievalConfig), ConfigHash: input.ConfigHash,
		SourceContentVersion: input.SourceContentVersion, IndexedContentVersion: input.IndexedContentVersion,
		Status: input.Status, ManualEditDisposition: disposition, CreatedAt: time.Now().UTC(),
	}, nil
}

// ValidateActivation enforces the active-pointer/content-version CAS.
func (g *IndexGeneration) ValidateActivation(activeGenerationID uuid.UUID, contentVersion int64, archiveManualEdits bool) error {
	if g == nil || g.Status != value.IndexGenerationReady {
		return domainerrors.ErrGenerationNotReady
	}
	if g.BaseGenerationID == nil || *g.BaseGenerationID != activeGenerationID || g.SourceContentVersion != contentVersion {
		return domainerrors.ErrGenerationStale
	}
	if g.ManualEditDisposition == value.ManualEditPending && !archiveManualEdits {
		return domainerrors.ErrManualEditConfirmationRequired
	}
	if contentVersion < 0 {
		return fmt.Errorf("%w: content_version 不能为负数", domainerrors.ErrValidation)
	}
	return nil
}

func supportedEmbeddingDimension(dimension int) bool {
	switch dimension {
	case 798, 1024, 2048, 3584:
		return true
	default:
		return false
	}
}

func validIndexGenerationStatus(status value.IndexGenerationStatus) bool {
	switch status {
	case value.IndexGenerationBuilding, value.IndexGenerationReady, value.IndexGenerationStale,
		value.IndexGenerationFailed, value.IndexGenerationRetired:
		return true
	default:
		return false
	}
}

func cloneStringAnyMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}
