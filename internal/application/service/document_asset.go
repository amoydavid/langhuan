package service

import (
	"context"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// DocumentAssetReader 读取一个 Document 的图片资产。
type DocumentAssetReader interface {
	ListAssetsByRevision(ctx context.Context, workspaceID, revisionID uuid.UUID) ([]*model.Asset, error)
	// GetByID 按 workspace + asset ID 读取单个资产（供鉴权代理 handler）。
	GetByID(ctx context.Context, workspaceID, assetID uuid.UUID) (*model.Asset, error)
}

// DocumentWithRevisionReader 读取 Document 以获取其 active revision。
type DocumentWithRevisionReader interface {
	Get(ctx context.Context, workspaceID uuid.UUID, id uuid.UUID) (*model.Document, error)
}

// DocumentAssetService 提供按 Document 查询图片资产的能力。
// 资产按 active revision 归属（document_assets 是 revision 级事实）。
type DocumentAssetService struct {
	assets   DocumentAssetReader
	document DocumentWithRevisionReader
}

// NewDocumentAssetService 创建 DocumentAssetService。
// document 参数应传入实现 (ctx, workspaceID, id) 签名的 Document 读取器（如 db.DocumentRepository）。
func NewDocumentAssetService(assets DocumentAssetReader, document DocumentWithRevisionReader) *DocumentAssetService {
	return &DocumentAssetService{assets: assets, document: document}
}

// ListByDocument 返回指定 Document 当前 active revision 的图片资产。
// 无 active revision 或 revision 无资产时返回空列表（非错误）。
// access 校验文档所属知识库在授权集合内（受限主体越界统一 not_found）。
func (s *DocumentAssetService) ListByDocument(ctx context.Context, access value.ResourceAccess, documentID uuid.UUID) ([]*model.Asset, error) {
	if access.WorkspaceID == uuid.Nil || documentID == uuid.Nil {
		return nil, domainerrors.ErrValidation
	}
	doc, err := s.document.Get(ctx, access.WorkspaceID, documentID)
	if err != nil {
		return nil, err
	}
	if err := validateResourceAccess(access, access.WorkspaceID, doc.KnowledgeBaseID); err != nil {
		return nil, err
	}
	if doc.ActiveRevisionID == nil {
		return []*model.Asset{}, nil
	}
	assets, err := s.assets.ListAssetsByRevision(ctx, access.WorkspaceID, *doc.ActiveRevisionID)
	if err != nil {
		return nil, err
	}
	if assets == nil {
		assets = []*model.Asset{}
	}
	return assets, nil
}

// GetByID 按 workspace + asset ID 读取单个资产。
// access 校验资产所属知识库在授权集合内。
func (s *DocumentAssetService) GetByID(ctx context.Context, access value.ResourceAccess, assetID uuid.UUID) (*model.Asset, error) {
	if access.WorkspaceID == uuid.Nil || assetID == uuid.Nil {
		return nil, domainerrors.ErrValidation
	}
	asset, err := s.assets.GetByID(ctx, access.WorkspaceID, assetID)
	if err != nil {
		return nil, err
	}
	if err := validateResourceAccess(access, access.WorkspaceID, asset.KnowledgeBaseID); err != nil {
		return nil, err
	}
	return asset, nil
}
