package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// DocumentRevisionRepository persists immutable acquisition and parse facts.
type DocumentRevisionRepository struct {
	db *gorm.DB
}

// NewDocumentRevisionRepository creates a DocumentRevision repository.
func NewDocumentRevisionRepository(database *gorm.DB) *DocumentRevisionRepository {
	return &DocumentRevisionRepository{db: database}
}

// Get reads one revision through an explicit Workspace boundary.
func (r *DocumentRevisionRepository) Get(
	ctx context.Context,
	workspaceID, revisionID uuid.UUID,
) (*model.DocumentRevision, error) {
	var row DocumentRevisionRow
	if err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND id = ?", workspaceID, revisionID).
		First(&row).Error; err != nil {
		return nil, translateDBError(err, "读取 DocumentRevision 失败")
	}
	return documentRevisionFromRow(&row)
}

// CompleteParse records the first successful parse result and makes ready retries no-ops.
func (r *DocumentRevisionRepository) CompleteParse(
	ctx context.Context,
	workspaceID, revisionID uuid.UUID,
	markdown string,
	manifest model.ParseManifest,
) error {
	return NewWorkspaceTxRunner(r.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		var row DocumentRevisionRow
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("workspace_id = ? AND id = ?", workspaceID, revisionID).
			First(&row).Error; err != nil {
			return translateDBError(err, "锁定 DocumentRevision 失败")
		}
		if value.DocumentRevisionStatus(row.Status) == value.DocumentRevisionReady {
			return nil
		}
		kind := value.DocumentKind(row.Kind)
		if kind != value.DocumentKindFile && kind != value.DocumentKindWeb {
			return fmt.Errorf("%w: kind=%q 不进入通用解析器", domainerrors.ErrValidation, kind)
		}
		if err := manifest.Validate(markdown); err != nil {
			return fmt.Errorf("保存 DocumentRevision 解析结果失败: %w", err)
		}
		encodedManifest, err := parseManifestToJSONMap(manifest)
		if err != nil {
			return fmt.Errorf("保存 DocumentRevision 解析结果失败: %w", err)
		}
		now := time.Now().UTC()
		result := tx.WithContext(ctx).Model(&DocumentRevisionRow{}).
			Where("workspace_id = ? AND id = ?", workspaceID, revisionID).
			Updates(map[string]any{
				"normalized_markdown": markdown, "parse_manifest": encodedManifest,
				"status": string(value.DocumentRevisionReady), "error_class": "", "error_message": "",
				"completed_at": now,
			})
		if result.Error != nil {
			return translateDBError(result.Error, "完成 DocumentRevision 解析失败")
		}
		if result.RowsAffected == 0 {
			return domainerrors.ErrNotFound
		}
		return nil
	})
}
