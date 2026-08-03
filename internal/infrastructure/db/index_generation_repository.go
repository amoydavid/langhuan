package db

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/dajee/langhuan/internal/domain/model"
)

// IndexGenerationRepository reads immutable indexing configuration snapshots.
type IndexGenerationRepository struct {
	db *gorm.DB
}

// NewIndexGenerationRepository creates an IndexGeneration repository.
func NewIndexGenerationRepository(database *gorm.DB) *IndexGenerationRepository {
	return &IndexGenerationRepository{db: database}
}

// Get returns one generation through an explicit Workspace boundary.
func (r *IndexGenerationRepository) Get(
	ctx context.Context,
	workspaceID, generationID uuid.UUID,
) (*model.IndexGeneration, error) {
	var row IndexGenerationRow
	err := NewWorkspaceTxRunner(r.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).
			Where("workspace_id = ? AND id = ?", workspaceID, generationID).
			First(&row).Error; err != nil {
			return translateDBError(err, "读取 IndexGeneration 失败")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return indexGenerationFromRow(&row), nil
}
