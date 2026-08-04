package service

import (
	"context"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
)

// DocumentAssetReader 读取一个 Document 的图片资产。
type DocumentAssetReader interface {
	ListAssetsByRevision(ctx context.Context, workspaceID, revisionID uuid.UUID) ([]*model.Asset, error)
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
func (s *DocumentAssetService) ListByDocument(ctx context.Context, workspaceID, documentID uuid.UUID) ([]*model.Asset, error) {
	if workspaceID == uuid.Nil || documentID == uuid.Nil {
		return nil, domainerrors.ErrValidation
	}
	doc, err := s.document.Get(ctx, workspaceID, documentID)
	if err != nil {
		return nil, err
	}
	if doc.ActiveRevisionID == nil {
		return []*model.Asset{}, nil
	}
	assets, err := s.assets.ListAssetsByRevision(ctx, workspaceID, *doc.ActiveRevisionID)
	if err != nil {
		return nil, err
	}
	if assets == nil {
		assets = []*model.Asset{}
	}
	return assets, nil
}
