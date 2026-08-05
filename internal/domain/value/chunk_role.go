package value

import (
	"fmt"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// ChunkRole describes whether a chunk supplies context, retrieval evidence, or both.
type ChunkRole string

const (
	// ChunkRoleParent supplies full context for one or more child chunks.
	ChunkRoleParent ChunkRole = "parent"
	// ChunkRoleChild is a retrievable chunk that belongs to a parent chunk.
	ChunkRoleChild ChunkRole = "child"
	// ChunkRoleFlat is a retrievable standalone chunk when parent-child chunking is disabled.
	ChunkRoleFlat ChunkRole = "flat"
)

// Validate reports whether the role is supported by the chunking contract.
func (r ChunkRole) Validate() error {
	switch r {
	case ChunkRoleParent, ChunkRoleChild, ChunkRoleFlat:
		return nil
	default:
		return fmt.Errorf("%w: 不支持的分块角色 %q", domainerrors.ErrValidation, r)
	}
}

// IsRetrievable reports whether the role creates vector and full-text projections.
func (r ChunkRole) IsRetrievable() bool {
	return r == ChunkRoleChild || r == ChunkRoleFlat
}
