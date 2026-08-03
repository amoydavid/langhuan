package db

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
)

// FileTreeRepository implements tenant-local File tree transactions.
type FileTreeRepository struct {
	db *gorm.DB
}

// NewFileTreeRepository creates a File tree repository.
func NewFileTreeRepository(database *gorm.DB) *FileTreeRepository {
	return &FileTreeRepository{db: database}
}

func (r *FileTreeRepository) WithinWorkspace(
	ctx context.Context,
	workspaceID uuid.UUID,
	fn func(context.Context, service.FileTreeTx) error,
) error {
	return NewWorkspaceTxRunner(r.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		return fn(ctx, &fileTreeTx{db: tx, workspaceID: workspaceID})
	})
}

type fileTreeTx struct {
	db          *gorm.DB
	workspaceID uuid.UUID
}

func (tx *fileTreeTx) ListFileTreeNodes(ctx context.Context, knowledgeBaseID uuid.UUID) ([]*model.FileTreeNode, error) {
	var rows []FileTreeNodeRow
	if err := tx.db.WithContext(ctx).
		Where("workspace_id = ? AND knowledge_base_id = ?", tx.workspaceID, knowledgeBaseID).
		Order("created_at, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]*model.FileTreeNode, len(rows))
	for index := range rows {
		result[index] = fileTreeNodeFromRow(&rows[index])
	}
	return result, nil
}

func (tx *fileTreeTx) GetFileTreeNodeForUpdate(ctx context.Context, id uuid.UUID) (*model.FileTreeNode, error) {
	var row FileTreeNodeRow
	if err := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&row, "workspace_id = ? AND id = ?", tx.workspaceID, id).Error; err != nil {
		return nil, translateDBError(err, "锁定文件树节点失败")
	}
	return fileTreeNodeFromRow(&row), nil
}

func (tx *fileTreeTx) GetDocumentForUpdate(ctx context.Context, id uuid.UUID) (*model.Document, error) {
	var row DocumentRow
	if err := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&row, "workspace_id = ? AND id = ?", tx.workspaceID, id).Error; err != nil {
		return nil, translateDBError(err, "锁定 File Document 失败")
	}
	return documentV2FromRow(&row), nil
}

func (tx *fileTreeTx) WouldCreateCycle(ctx context.Context, nodeID, newParentID uuid.UUID) (bool, error) {
	var cycle bool
	err := tx.db.WithContext(ctx).Raw(`
WITH RECURSIVE descendants AS (
    SELECT id, knowledge_base_id FROM file_tree_nodes
    WHERE workspace_id = ? AND id = ?
    UNION ALL
    SELECT child.id, child.knowledge_base_id
    FROM file_tree_nodes child
    JOIN descendants d ON child.parent_id = d.id
    WHERE child.workspace_id = ? AND child.knowledge_base_id = d.knowledge_base_id
)
SELECT EXISTS (SELECT 1 FROM descendants WHERE id = ?)
`, tx.workspaceID, nodeID, tx.workspaceID, newParentID).Scan(&cycle).Error
	return cycle, err
}

func (tx *fileTreeTx) HasFileTreeChildren(ctx context.Context, id uuid.UUID) (bool, error) {
	var row FileTreeNodeRow
	err := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("workspace_id = ? AND parent_id = ?", tx.workspaceID, id).
		Order("id").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (tx *fileTreeTx) CreateFileTreeNode(ctx context.Context, node *model.FileTreeNode) error {
	if node.WorkspaceID != tx.workspaceID {
		return domainerrors.ErrNotFound
	}
	if err := tx.db.WithContext(ctx).Create(fileTreeNodeToRow(node)).Error; err != nil {
		return mapFileTreeWriteError(err, "创建文件树节点失败")
	}
	return nil
}

func (tx *fileTreeTx) SaveFileTreeNode(ctx context.Context, node *model.FileTreeNode, document *model.Document) error {
	if node.WorkspaceID != tx.workspaceID || (document != nil && document.WorkspaceID != tx.workspaceID) {
		return domainerrors.ErrNotFound
	}
	result := tx.db.WithContext(ctx).Model(&FileTreeNodeRow{}).
		Where("workspace_id = ? AND id = ?", tx.workspaceID, node.ID).
		Updates(map[string]any{"parent_id": node.ParentID, "name": node.Name, "updated_at": node.UpdatedAt})
	if result.Error != nil {
		return mapFileTreeWriteError(result.Error, "更新文件树节点失败")
	}
	if result.RowsAffected == 0 {
		return domainerrors.ErrNotFound
	}
	if document != nil {
		result = tx.db.WithContext(ctx).Model(&DocumentRow{}).
			Where("workspace_id = ? AND id = ?", tx.workspaceID, document.ID).
			Updates(map[string]any{"title": document.Title, "updated_at": document.UpdatedAt})
		if result.Error != nil {
			return mapFileTreeWriteError(result.Error, "同步 File Document 标题失败")
		}
		if result.RowsAffected == 0 {
			return domainerrors.ErrNotFound
		}
	}
	return nil
}

func (tx *fileTreeTx) DeleteFileTreeNode(ctx context.Context, id uuid.UUID) error {
	result := tx.db.WithContext(ctx).Delete(&FileTreeNodeRow{}, "workspace_id = ? AND id = ?", tx.workspaceID, id)
	if result.Error != nil {
		return mapFileTreeWriteError(result.Error, "删除文件树节点失败")
	}
	if result.RowsAffected == 0 {
		return domainerrors.ErrNotFound
	}
	return nil
}

func mapFileTreeWriteError(err error, operation string) error {
	mapped := translateDBError(err, operation)
	if errors.Is(mapped, domainerrors.ErrConflict) {
		return domainerrors.ErrFileTreeNameConflict
	}
	return mapped
}
