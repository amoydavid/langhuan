package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

type DocumentRepository interface {
	Get(ctx context.Context, workspaceID uuid.UUID, id uuid.UUID) (*model.Document, error)
	List(ctx context.Context, filter DocumentListFilter) ([]*model.Document, error)
	Delete(ctx context.Context, workspaceID, documentID uuid.UUID) error
}

// DocumentListFilter scopes document listing and optionally narrows by kind.
type DocumentListFilter struct {
	WorkspaceID     uuid.UUID
	KnowledgeBaseID uuid.UUID
	Kind            *value.DocumentKind
}

// Delete soft-deletes a Document and atomically removes it from retrieval and the File Tree.
// access 用于 API Key 主体把绑定集合下推为 404 边界；Session 主体传 Unrestricted。
func (s *DocumentService) Delete(ctx context.Context, access value.ResourceAccess, documentID uuid.UUID) error {
	if access.WorkspaceID == uuid.Nil || documentID == uuid.Nil {
		return fmt.Errorf("%w: workspace_id/document_id 不能为空", domainerrors.ErrValidation)
	}
	// 受限主体先校验目标 Document 的知识库是否在允许集合内，越界统一 404。
	if !access.Unrestricted {
		doc, err := s.repo.Get(ctx, access.WorkspaceID, documentID)
		if err != nil {
			return err
		}
		if !access.AllowsKnowledgeBase(doc.KnowledgeBaseID) {
			return domainerrors.ErrNotFound
		}
	}
	return s.repo.Delete(ctx, access.WorkspaceID, documentID)
}

type KnowledgeBaseReader interface {
	Get(ctx context.Context, workspaceID uuid.UUID, id uuid.UUID) (*model.KnowledgeBase, error)
}

type DocumentService struct {
	repo           DocumentRepository
	knowledgeBases KnowledgeBaseReader
}

func NewDocumentService(repo DocumentRepository, knowledgeBases KnowledgeBaseReader) *DocumentService {
	return &DocumentService{repo: repo, knowledgeBases: knowledgeBases}
}

func (s *DocumentService) List(ctx context.Context, filter DocumentListFilter) ([]*dto.Document, error) {
	if filter.WorkspaceID == uuid.Nil || filter.KnowledgeBaseID == uuid.Nil {
		return nil, fmt.Errorf("%w: 文档列表 lineage 无效", domainerrors.ErrValidation)
	}
	if filter.Kind != nil {
		if err := filter.Kind.Validate(); err != nil {
			return nil, fmt.Errorf("%w: 文档 kind 无效", domainerrors.ErrValidation)
		}
	}
	if _, err := s.knowledgeBases.Get(ctx, filter.WorkspaceID, filter.KnowledgeBaseID); err != nil {
		return nil, err
	}
	items, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	result := make([]*dto.Document, 0, len(items))
	for _, doc := range items {
		result = append(result, dto.DocumentFromModel(doc))
	}
	return result, nil
}

// Get 读取单条 Document。access 用于 API Key 主体把绑定集合下推为 404 边界。
func (s *DocumentService) Get(ctx context.Context, access value.ResourceAccess, id uuid.UUID) (*dto.Document, error) {
	doc, err := s.repo.Get(ctx, access.WorkspaceID, id)
	if err != nil {
		return nil, err
	}
	if !access.Unrestricted && !access.AllowsKnowledgeBase(doc.KnowledgeBaseID) {
		return nil, domainerrors.ErrNotFound
	}
	return dto.DocumentFromModel(doc), nil
}
