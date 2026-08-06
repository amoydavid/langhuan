package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
)

// KnowledgeBaseModelBinder resolves the model snapshot for a knowledge base.
type KnowledgeBaseModelBinder interface {
	Create(context.Context, *model.KnowledgeBase) (*model.ResolvedModel, error)
	ResolveSelectable(context.Context, uuid.UUID, uuid.UUID) (*model.ResolvedModel, error)
	GetResolved(context.Context, uuid.UUID, uuid.UUID) (*model.ResolvedKnowledgeBase, error)
	ListResolved(context.Context, uuid.UUID, []uuid.UUID) ([]*model.ResolvedKnowledgeBase, error)
}
