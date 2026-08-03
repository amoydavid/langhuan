package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// FAQRepository persists complete FAQ revision aggregates in Workspace transactions.
type FAQRepository struct {
	db *gorm.DB
}

// NewFAQRepository creates an FAQ aggregate repository.
func NewFAQRepository(database *gorm.DB) *FAQRepository {
	return &FAQRepository{db: database}
}

// WithinWorkspace runs an FAQ aggregate operation with tenant-local context.
func (r *FAQRepository) WithinWorkspace(
	ctx context.Context,
	workspaceID uuid.UUID,
	fn func(context.Context, appservice.FAQRevisionTx) error,
) error {
	return NewWorkspaceTxRunner(r.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		return fn(ctx, &faqRevisionTx{db: tx, workspaceID: workspaceID})
	})
}

// GetFAQRevision reads one complete FAQ aggregate through an explicit Workspace boundary.
func (r *FAQRepository) GetFAQRevision(
	ctx context.Context,
	workspaceID, revisionID uuid.UUID,
) (*model.FAQRevision, error) {
	var faq *model.FAQRevision
	err := NewWorkspaceTxRunner(r.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		var err error
		faq, err = (&faqRevisionTx{db: tx, workspaceID: workspaceID}).GetFAQRevision(ctx, revisionID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return faq, nil
}

type faqRevisionTx struct {
	db          *gorm.DB
	workspaceID uuid.UUID
}

func (tx *faqRevisionTx) GetKnowledgeBase(ctx context.Context, id uuid.UUID) (*model.KnowledgeBase, error) {
	var row KnowledgeBaseRow
	if err := tx.db.WithContext(ctx).
		Where("workspace_id = ? AND id = ? AND deleted_at IS NULL", tx.workspaceID, id).
		First(&row).Error; err != nil {
		return nil, translateDBError(err, "读取 FAQ KnowledgeBase 失败")
	}
	return knowledgeBaseV2FromRow(&row), nil
}

func (tx *faqRevisionTx) GetDocumentForUpdate(ctx context.Context, id uuid.UUID) (*model.Document, error) {
	var row DocumentRow
	if err := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("workspace_id = ? AND id = ? AND deleted_at IS NULL", tx.workspaceID, id).
		First(&row).Error; err != nil {
		return nil, translateDBError(err, "锁定 FAQ Document 失败")
	}
	return documentV2FromRow(&row), nil
}

func (tx *faqRevisionTx) GetFAQRevision(ctx context.Context, id uuid.UUID) (*model.FAQRevision, error) {
	var revisionRow DocumentRevisionRow
	if err := tx.db.WithContext(ctx).
		Where("workspace_id = ? AND id = ? AND kind = ?", tx.workspaceID, id, value.DocumentKindFAQ).
		First(&revisionRow).Error; err != nil {
		return nil, translateDBError(err, "读取 FAQ DocumentRevision 失败")
	}
	revision, err := documentRevisionFromRow(&revisionRow)
	if err != nil {
		return nil, err
	}
	var contentRow FAQRevisionContentRow
	if err := tx.db.WithContext(ctx).
		Where("workspace_id = ? AND document_revision_id = ?", tx.workspaceID, id).
		First(&contentRow).Error; err != nil {
		return nil, translateDBError(err, "读取 FAQ answer 失败")
	}
	var questionRows []FAQRevisionQuestionRow
	if err := tx.db.WithContext(ctx).
		Where("workspace_id = ? AND document_revision_id = ?", tx.workspaceID, id).
		Order("sequence ASC").Find(&questionRows).Error; err != nil {
		return nil, translateDBError(err, "读取 FAQ questions 失败")
	}
	if len(questionRows) == 0 {
		return nil, fmt.Errorf("%w: FAQ questions 为空", domainerrors.ErrValidation)
	}
	return faqRevisionFromRows(revision, &contentRow, questionRows)
}

func (tx *faqRevisionTx) CreateFAQRevisionAggregate(
	ctx context.Context,
	document *model.Document,
	faq *model.FAQRevision,
	job *model.Job,
) error {
	if err := validateFAQPersistenceAggregate(tx.workspaceID, document, faq, job); err != nil {
		return err
	}
	revisionRow, err := documentRevisionToRow(faq.DocumentRevision)
	if err != nil {
		return err
	}
	contentRow, questionRows := faqRevisionToRows(faq)

	var existing DocumentRow
	err = tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("workspace_id = ? AND id = ?", tx.workspaceID, document.ID).
		First(&existing).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		if faq.DocumentRevision.RevisionNo != 1 {
			return domainerrors.ErrNotFound
		}
		if err := tx.db.WithContext(ctx).Create(documentV2ToRow(document)).Error; err != nil {
			return translateDBError(err, "创建 FAQ Document 失败")
		}
	case err != nil:
		return translateDBError(err, "锁定 FAQ Document 失败")
	default:
		if existing.KnowledgeBaseID != document.KnowledgeBaseID || existing.Kind != string(value.DocumentKindFAQ) {
			return domainerrors.ErrNotFound
		}
		if err := tx.db.WithContext(ctx).Model(&DocumentRow{}).
			Where("workspace_id = ? AND id = ?", tx.workspaceID, document.ID).
			Updates(map[string]any{
				"status": string(document.Status), "updated_at": document.UpdatedAt,
			}).Error; err != nil {
			return translateDBError(err, "更新 FAQ Document 状态失败")
		}
	}
	if err := tx.db.WithContext(ctx).Create(revisionRow).Error; err != nil {
		return translateDBError(err, "创建 FAQ DocumentRevision 失败")
	}
	if err := tx.db.WithContext(ctx).Create(contentRow).Error; err != nil {
		return translateDBError(err, "创建 FAQ answer 失败")
	}
	if err := tx.db.WithContext(ctx).CreateInBatches(questionRows, 200).Error; err != nil {
		return translateDBError(err, "批量创建 FAQ questions 失败")
	}
	if err := tx.db.WithContext(ctx).Create(jobV2ToRow(job)).Error; err != nil {
		return translateDBError(err, "创建 FAQ index Job 失败")
	}
	return nil
}

func validateFAQPersistenceAggregate(
	workspaceID uuid.UUID,
	document *model.Document,
	faq *model.FAQRevision,
	job *model.Job,
) error {
	if document == nil || faq == nil || faq.DocumentRevision == nil || job == nil ||
		document.WorkspaceID != workspaceID || faq.DocumentRevision.WorkspaceID != workspaceID ||
		job.WorkspaceID != workspaceID || document.Kind != value.DocumentKindFAQ ||
		faq.DocumentRevision.Kind != value.DocumentKindFAQ || faq.DocumentRevision.DocumentID != document.ID ||
		job.DocumentID != document.ID || job.DocumentRevisionID != faq.DocumentRevision.ID ||
		document.KnowledgeBaseID != faq.DocumentRevision.KnowledgeBaseID ||
		document.KnowledgeBaseID != job.KnowledgeBaseID || len(faq.Questions) == 0 {
		return fmt.Errorf("%w: FAQ persistence lineage 无效", domainerrors.ErrValidation)
	}
	for index, question := range faq.Questions {
		if question.Sequence != index || question.WorkspaceID != workspaceID ||
			question.KnowledgeBaseID != document.KnowledgeBaseID || question.DocumentID != document.ID ||
			question.DocumentRevisionID != faq.DocumentRevision.ID {
			return fmt.Errorf("%w: FAQ question %d lineage/sequence 无效", domainerrors.ErrValidation, index)
		}
	}
	return nil
}
