package service

import (
	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// validateResourceAccess enforces the final workspace/knowledge-base boundary
// inside application services. A zero ResourceAccess is accepted for legacy
// in-process callers and treated as unrestricted; HTTP callers always provide
// the authenticated access context.
func validateResourceAccess(access value.ResourceAccess, workspaceID, knowledgeBaseID uuid.UUID) error {
	if workspaceID == uuid.Nil || knowledgeBaseID == uuid.Nil {
		return domainerrors.ErrValidation
	}
	if access.WorkspaceID != uuid.Nil && access.WorkspaceID != workspaceID {
		return domainerrors.ErrNotFound
	}
	if access.WorkspaceID != uuid.Nil && !access.Unrestricted && !access.AllowsKnowledgeBase(knowledgeBaseID) {
		return domainerrors.ErrNotFound
	}
	return nil
}
