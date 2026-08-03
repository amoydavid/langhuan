package model

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// NewChunkRevisionInput contains one immutable effective chunk revision.
type NewChunkRevisionInput struct {
	WorkspaceID        uuid.UUID
	KnowledgeBaseID    uuid.UUID
	DocumentID         uuid.UUID
	DocumentRevisionID uuid.UUID
	ChunkSetID         uuid.UUID
	ChunkID            uuid.UUID
	RevisionNo         int64
	BaseRevisionID     *uuid.UUID
	Content            string
	ContextHeader      string
	EmbeddingContent   string
	Enabled            bool
	Status             value.ChunkRevisionStatus
	EditSource         value.ChunkEditSource
	EditorUserID       *uuid.UUID
}

// ChunkRevision stores the effective text and complete edit audit.
type ChunkRevision struct {
	ID                 uuid.UUID
	WorkspaceID        uuid.UUID
	KnowledgeBaseID    uuid.UUID
	DocumentID         uuid.UUID
	DocumentRevisionID uuid.UUID
	ChunkSetID         uuid.UUID
	ChunkID            uuid.UUID
	RevisionNo         int64
	BaseRevisionID     *uuid.UUID
	Content            string
	ContextHeader      string
	EmbeddingContent   string
	Enabled            bool
	Status             value.ChunkRevisionStatus
	EditSource         value.ChunkEditSource
	EditorUserID       *uuid.UUID
	ErrorClass         string
	ErrorMessage       string
	CreatedAt          time.Time
	IndexedAt          *time.Time
}

// NewChunkRevision validates lineage and system/user edit invariants.
func NewChunkRevision(input NewChunkRevisionInput) (*ChunkRevision, error) {
	if input.WorkspaceID == uuid.Nil || input.KnowledgeBaseID == uuid.Nil || input.DocumentID == uuid.Nil ||
		input.DocumentRevisionID == uuid.Nil || input.ChunkSetID == uuid.Nil || input.ChunkID == uuid.Nil {
		return nil, fmt.Errorf("%w: ChunkRevision lineage 不能为空", domainerrors.ErrValidation)
	}
	if input.RevisionNo < 1 || !validChunkRevisionStatus(input.Status) {
		return nil, fmt.Errorf("%w: ChunkRevision revision/status 无效", domainerrors.ErrValidation)
	}
	if input.Enabled && (strings.TrimSpace(input.Content) == "" || strings.TrimSpace(input.EmbeddingContent) == "") {
		return nil, fmt.Errorf("%w: 启用的 ChunkRevision 内容不能为空", domainerrors.ErrValidation)
	}
	switch input.EditSource {
	case value.ChunkEditSourceSystem:
		if input.EditorUserID != nil {
			return nil, fmt.Errorf("%w: system revision 不能有 editor", domainerrors.ErrValidation)
		}
	case value.ChunkEditSourceUser:
		if nilOrEmptyUUID(input.BaseRevisionID) || nilOrEmptyUUID(input.EditorUserID) {
			return nil, fmt.Errorf("%w: user revision 必须有 base 与 editor", domainerrors.ErrValidation)
		}
	default:
		return nil, fmt.Errorf("%w: Chunk edit source 无效", domainerrors.ErrValidation)
	}
	return &ChunkRevision{
		ID: uuid.New(), WorkspaceID: input.WorkspaceID, KnowledgeBaseID: input.KnowledgeBaseID,
		DocumentID: input.DocumentID, DocumentRevisionID: input.DocumentRevisionID,
		ChunkSetID: input.ChunkSetID, ChunkID: input.ChunkID, RevisionNo: input.RevisionNo,
		BaseRevisionID: input.BaseRevisionID, Content: input.Content, ContextHeader: input.ContextHeader,
		EmbeddingContent: input.EmbeddingContent, Enabled: input.Enabled, Status: input.Status,
		EditSource: input.EditSource, EditorUserID: input.EditorUserID, CreatedAt: time.Now().UTC(),
	}, nil
}

func validChunkRevisionStatus(status value.ChunkRevisionStatus) bool {
	switch status {
	case value.ChunkRevisionPending, value.ChunkRevisionIndexing,
		value.ChunkRevisionReady, value.ChunkRevisionFailed:
		return true
	default:
		return false
	}
}
