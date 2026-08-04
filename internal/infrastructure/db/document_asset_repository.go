package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/dajee/langhuan/internal/domain/model"
)

// DocumentAssetRepository persists image assets discovered during document parsing.
type DocumentAssetRepository struct {
	db *gorm.DB
}

// NewDocumentAssetRepository creates a DocumentAsset repository.
func NewDocumentAssetRepository(database *gorm.DB) *DocumentAssetRepository {
	return &DocumentAssetRepository{db: database}
}

// DeleteAssetsByRevision removes all assets belonging to one revision.
// It is called before writing new assets on re-parse to ensure idempotency.
func (r *DocumentAssetRepository) DeleteAssetsByRevision(
	ctx context.Context,
	workspaceID, revisionID uuid.UUID,
) error {
	return r.db.WithContext(ctx).
		Where("workspace_id = ? AND document_revision_id = ?", workspaceID, revisionID).
		Delete(&DocumentAssetRow{}).Error
}

// CreateAssets inserts a batch of assets for one revision.
func (r *DocumentAssetRepository) CreateAssets(
	ctx context.Context,
	assets []model.Asset,
) error {
	if len(assets) == 0 {
		return nil
	}
	now := time.Now().UTC()
	rows := make([]DocumentAssetRow, 0, len(assets))
	for _, asset := range assets {
		rows = append(rows, documentAssetToRow(&asset, now))
	}
	return r.db.WithContext(ctx).Create(&rows).Error
}

// ListAssetsByRevision returns all assets for one revision, ordered by creation time.
func (r *DocumentAssetRepository) ListAssetsByRevision(
	ctx context.Context,
	workspaceID, revisionID uuid.UUID,
) ([]*model.Asset, error) {
	var rows []DocumentAssetRow
	if err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND document_revision_id = ?", workspaceID, revisionID).
		Order("created_at ASC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, translateDBError(err, "读取 DocumentAssets 失败")
	}
	result := make([]*model.Asset, 0, len(rows))
	for i := range rows {
		result = append(result, documentAssetFromRow(&rows[i]))
	}
	return result, nil
}

// GetByID 按 workspace + asset ID 查询单个资产，供鉴权代理 handler 定位 storage key。
func (r *DocumentAssetRepository) GetByID(
	ctx context.Context,
	workspaceID, assetID uuid.UUID,
) (*model.Asset, error) {
	var row DocumentAssetRow
	err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND id = ?", workspaceID, assetID).
		First(&row).Error
	if err != nil {
		return nil, translateDBError(err, "读取 DocumentAsset 失败")
	}
	return documentAssetFromRow(&row), nil
}

func documentAssetToRow(asset *model.Asset, now time.Time) DocumentAssetRow {
	createdAt := asset.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	return DocumentAssetRow{
		ID:                 asset.ID,
		WorkspaceID:        asset.WorkspaceID,
		KnowledgeBaseID:    asset.KnowledgeBaseID,
		DocumentID:         asset.DocumentID,
		DocumentRevisionID: asset.DocumentRevisionID,
		OriginalRef:        asset.OriginalRef,
		StorageKey:         asset.StorageKey,
		PublicURL:          asset.PublicURL,
		MimeType:           asset.MimeType,
		SHA256:             asset.SHA256,
		SizeBytes:          asset.SizeBytes,
		Metadata:           JSONMap(cloneAssetMetadata(asset.Metadata)),
		CreatedAt:          createdAt,
	}
}

func documentAssetFromRow(row *DocumentAssetRow) *model.Asset {
	return &model.Asset{
		ID:                 row.ID,
		WorkspaceID:        row.WorkspaceID,
		KnowledgeBaseID:    row.KnowledgeBaseID,
		DocumentID:         row.DocumentID,
		DocumentRevisionID: row.DocumentRevisionID,
		OriginalRef:        row.OriginalRef,
		StorageKey:         row.StorageKey,
		PublicURL:          row.PublicURL,
		MimeType:           row.MimeType,
		SHA256:             row.SHA256,
		SizeBytes:          row.SizeBytes,
		Metadata:           cloneAssetMetadata(row.Metadata),
		CreatedAt:          row.CreatedAt,
	}
}

// cloneAssetMetadata returns a safe copy of the metadata map, handling nil.
func cloneAssetMetadata(src map[string]any) map[string]any {
	if src == nil {
		return map[string]any{}
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
