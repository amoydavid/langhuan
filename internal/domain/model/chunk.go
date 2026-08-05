package model

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

type Chunk struct {
	ID                 uuid.UUID
	WorkspaceID        uuid.UUID
	KnowledgeBaseID    uuid.UUID
	DocumentID         uuid.UUID
	DocumentRevisionID uuid.UUID
	ChunkSetID         uuid.UUID
	Role               value.ChunkRole
	ParentChunkID      *uuid.UUID
	Sequence           int
	SourceContent      string
	ActiveRevisionID   *uuid.UUID
	Content            string
	EmbeddingContent   string
	ContextHeader      string
	SourceAnchor       value.SourceAnchor
	Metadata           map[string]any
	CreatedAt          time.Time
}

// ValidateLineage validates the parent-child relationship for a chunk.
func (c Chunk) ValidateLineage() error {
	if err := c.Role.Validate(); err != nil {
		return err
	}
	switch c.Role {
	case value.ChunkRoleChild:
		if c.ParentChunkID == nil {
			return fmt.Errorf("%w: 子块必须关联父块", domainerrors.ErrValidation)
		}
	case value.ChunkRoleParent, value.ChunkRoleFlat:
		if c.ParentChunkID != nil {
			return fmt.Errorf("%w: %s 分块不能关联父块", domainerrors.ErrValidation, c.Role)
		}
	}
	return nil
}
