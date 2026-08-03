package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// WorkspaceReadinessFacts is the persistence projection used to derive one next action.
type WorkspaceReadinessFacts struct {
	HasActiveProvider, HasSelectableEmbeddingModel bool
	KnowledgeBaseCount                             int64
	TotalDocuments, ReadyDocuments                 int64
	ProcessingDocuments, FailedDocuments           int64
	SearchableKnowledgeBaseCount                   int64
	RecommendedKnowledgeBaseID                     *uuid.UUID
	RecommendedKnowledgeBaseName                   string
	RecommendedDocumentID                          *uuid.UUID
	RecommendedDocumentName                        string
}

// WorkspaceReadinessStore reads Workspace-scoped readiness facts.
type WorkspaceReadinessStore interface {
	GetWorkspaceReadinessFacts(context.Context, uuid.UUID) (*WorkspaceReadinessFacts, error)
}

// WorkspaceReadinessService derives setup guidance from persisted facts.
type WorkspaceReadinessService struct{ store WorkspaceReadinessStore }

// NewWorkspaceReadinessService creates a Workspace readiness query service.
func NewWorkspaceReadinessService(store WorkspaceReadinessStore) *WorkspaceReadinessService {
	return &WorkspaceReadinessService{store: store}
}

// Get returns current facts and the highest-priority actionable next step.
func (s *WorkspaceReadinessService) Get(ctx context.Context, workspaceID uuid.UUID) (*dto.WorkspaceReadiness, error) {
	if workspaceID == uuid.Nil || s.store == nil {
		return nil, fmt.Errorf("%w: Workspace readiness 参数无效", domainerrors.ErrValidation)
	}
	facts, err := s.store.GetWorkspaceReadinessFacts(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if facts == nil {
		return nil, fmt.Errorf("%w: Workspace readiness facts 为空", domainerrors.ErrConflict)
	}
	return &dto.WorkspaceReadiness{
		HasActiveProvider:           facts.HasActiveProvider,
		HasSelectableEmbeddingModel: facts.HasSelectableEmbeddingModel,
		KnowledgeBaseCount:          facts.KnowledgeBaseCount,
		DocumentCounts: dto.WorkspaceReadinessDocumentCounts{
			Total: facts.TotalDocuments, Ready: facts.ReadyDocuments,
			Processing: facts.ProcessingDocuments, Failed: facts.FailedDocuments,
		},
		SearchableKnowledgeBaseCount: facts.SearchableKnowledgeBaseCount,
		RecommendedAction:            recommendedReadinessAction(*facts),
		RecommendedKnowledgeBaseID:   facts.RecommendedKnowledgeBaseID,
		RecommendedKnowledgeBaseName: facts.RecommendedKnowledgeBaseName,
		RecommendedDocumentID:        facts.RecommendedDocumentID,
		RecommendedDocumentName:      facts.RecommendedDocumentName,
	}, nil
}

func recommendedReadinessAction(facts WorkspaceReadinessFacts) dto.ReadinessAction {
	switch {
	case !facts.HasActiveProvider:
		return dto.ReadinessConfigureProvider
	case !facts.HasSelectableEmbeddingModel:
		return dto.ReadinessCreateEmbeddingModel
	case facts.KnowledgeBaseCount == 0:
		return dto.ReadinessCreateKnowledgeBase
	case facts.TotalDocuments == 0:
		return dto.ReadinessAddContent
	case facts.FailedDocuments > 0:
		return dto.ReadinessResolveFailedDocument
	case facts.ProcessingDocuments > 0 && facts.ReadyDocuments == 0:
		return dto.ReadinessWaitForProcessing
	case facts.SearchableKnowledgeBaseCount > 0:
		return dto.ReadinessTestRetrieval
	default:
		return dto.ReadinessNone
	}
}
