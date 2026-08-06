package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

type DocumentRepository struct {
	db *gorm.DB
}

func NewDocumentRepository(db *gorm.DB) *DocumentRepository {
	return &DocumentRepository{db: db}
}

func (r *DocumentRepository) Create(ctx context.Context, document *model.Document) error {
	row, err := documentToRow(document)
	if err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return fmt.Errorf("创建文档失败: %w", err)
	}
	return nil
}

func (r *DocumentRepository) Get(ctx context.Context, workspaceID uuid.UUID, id uuid.UUID) (*model.Document, error) {
	var document *model.Document
	err := NewWorkspaceTxRunner(r.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		var row DocumentRow
		if err := tx.WithContext(ctx).
			Where("workspace_id = ? AND id = ? AND deleted_at IS NULL", workspaceID, id).
			First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrRepositoryNotFound
			}
			return fmt.Errorf("读取文档失败: %w", err)
		}
		var err error
		document, err = documentFromRow(&row)
		if err != nil {
			return err
		}
		return loadDocumentActiveRevisions(ctx, tx, workspaceID, []*model.Document{document})
	})
	if err != nil {
		return nil, err
	}
	return document, nil
}

func (r *DocumentRepository) List(ctx context.Context, filter appservice.DocumentListFilter) ([]*model.Document, error) {
	result := make([]*model.Document, 0)
	err := NewWorkspaceTxRunner(r.db).WithinWorkspace(ctx, filter.WorkspaceID, func(tx *gorm.DB) error {
		var rows []DocumentRow
		query := tx.WithContext(ctx).
			Where("workspace_id = ? AND knowledge_base_id = ? AND deleted_at IS NULL", filter.WorkspaceID, filter.KnowledgeBaseID)
		if filter.Kind != nil {
			query = query.Where("kind = ?", *filter.Kind)
		}
		if err := query.
			Order("documents.created_at DESC, documents.id DESC").
			Find(&rows).Error; err != nil {
			return fmt.Errorf("列出文档失败: %w", err)
		}
		result = make([]*model.Document, 0, len(rows))
		for index := range rows {
			document, err := documentFromRow(&rows[index])
			if err != nil {
				return err
			}
			result = append(result, document)
		}
		return loadDocumentActiveRevisions(ctx, tx, filter.WorkspaceID, result)
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func loadDocumentActiveRevisions(
	ctx context.Context,
	tx *gorm.DB,
	workspaceID uuid.UUID,
	documents []*model.Document,
) error {
	revisionIDs := make([]uuid.UUID, 0, len(documents))
	for _, document := range documents {
		if document.ActiveRevisionID != nil {
			revisionIDs = append(revisionIDs, *document.ActiveRevisionID)
		}
	}
	if len(revisionIDs) == 0 {
		return nil
	}
	var rows []DocumentRevisionRow
	if err := tx.WithContext(ctx).Where(
		"workspace_id = ? AND id IN ?", workspaceID, revisionIDs,
	).Find(&rows).Error; err != nil {
		return fmt.Errorf("读取文档活动 Revision 失败: %w", err)
	}
	byID := make(map[uuid.UUID]*model.DocumentRevision, len(rows))
	for index := range rows {
		revision, err := documentRevisionFromRow(&rows[index])
		if err != nil {
			return err
		}
		byID[revision.ID] = revision
	}
	for _, document := range documents {
		if document.ActiveRevisionID == nil {
			continue
		}
		revision := byID[*document.ActiveRevisionID]
		if revision == nil || revision.DocumentID != document.ID || revision.Kind != document.Kind {
			return fmt.Errorf("文档活动 Revision lineage 不完整: %w", domainerrors.ErrConflict)
		}
		document.ActiveRevision = revision
	}
	return loadActiveFAQQuestionCounts(ctx, tx, workspaceID, documents)
}

func loadActiveFAQQuestionCounts(
	ctx context.Context,
	tx *gorm.DB,
	workspaceID uuid.UUID,
	documents []*model.Document,
) error {
	revisionIDs := make([]uuid.UUID, 0)
	byRevisionID := make(map[uuid.UUID]*model.Document)
	for _, document := range documents {
		if document.Kind != value.DocumentKindFAQ || document.ActiveRevision == nil {
			continue
		}
		revisionID := document.ActiveRevision.ID
		revisionIDs = append(revisionIDs, revisionID)
		byRevisionID[revisionID] = document
	}
	if len(revisionIDs) == 0 {
		return nil
	}
	type faqQuestionCountRow struct {
		DocumentRevisionID uuid.UUID `gorm:"column:document_revision_id"`
		Count              int64     `gorm:"column:question_count"`
	}
	var counts []faqQuestionCountRow
	if err := tx.WithContext(ctx).Model(&FAQRevisionQuestionRow{}).
		Select("document_revision_id, COUNT(*) AS question_count").
		Where("workspace_id = ? AND document_revision_id IN ?", workspaceID, revisionIDs).
		Group("document_revision_id").
		Find(&counts).Error; err != nil {
		return fmt.Errorf("读取 FAQ 问题数量失败: %w", err)
	}
	for _, count := range counts {
		document := byRevisionID[count.DocumentRevisionID]
		if document == nil || count.Count <= 0 {
			continue
		}
		document.FAQQuestionCount = int(count.Count)
	}
	for _, revisionID := range revisionIDs {
		if byRevisionID[revisionID].FAQQuestionCount == 0 {
			return fmt.Errorf("FAQ 活动 Revision questions 不完整: %w", domainerrors.ErrConflict)
		}
	}
	return nil
}

// Delete soft-deletes one Document after removing it from retrieval and, for File, the tree.
func (r *DocumentRepository) Delete(ctx context.Context, workspaceID, documentID uuid.UUID) error {
	return NewWorkspaceTxRunner(r.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		var row DocumentRow
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("workspace_id = ? AND id = ? AND deleted_at IS NULL", workspaceID, documentID).
			First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrRepositoryNotFound
			}
			return translateDBError(err, "锁定待删除文档失败")
		}

		now := time.Now().UTC()
		if err := tx.WithContext(ctx).Model(&RetrievalEntryRow{}).
			Where("workspace_id = ? AND document_id = ? AND state <> ?", workspaceID, documentID, value.RetrievalEntryRetired).
			Updates(map[string]any{
				"state": string(value.RetrievalEntryRetired), "retired_at": now,
			}).Error; err != nil {
			return translateDBError(err, "退役文档 RetrievalEntry 失败")
		}
		updated := tx.WithContext(ctx).Model(&DocumentRow{}).
			Where("workspace_id = ? AND id = ? AND deleted_at IS NULL", workspaceID, documentID).
			Updates(map[string]any{
				"status": string(value.DocumentStatusDeleted), "deleted_at": now, "updated_at": now,
			})
		if updated.Error != nil {
			return translateDBError(updated.Error, "软删除文档失败")
		}
		if updated.RowsAffected != 1 {
			return ErrRepositoryNotFound
		}
		if value.DocumentKind(row.Kind) != value.DocumentKindFile {
			return nil
		}
		deleted := tx.WithContext(ctx).Where(
			"workspace_id = ? AND knowledge_base_id = ? AND document_id = ? AND node_type = ?",
			workspaceID, row.KnowledgeBaseID, documentID, value.FileTreeNodeFile,
		).Delete(&FileTreeNodeRow{})
		if deleted.Error != nil {
			return translateDBError(deleted.Error, "删除 File Document 树节点失败")
		}
		if deleted.RowsAffected != 1 {
			return fmt.Errorf("文档文件树节点数量异常: %w", domainerrors.ErrConflict)
		}
		return nil
	})
}

func documentToRow(document *model.Document) (*DocumentRow, error) {
	return documentV2ToRow(document), nil
}

func documentFromRow(row *DocumentRow) (*model.Document, error) {
	return documentV2FromRow(row), nil
}
