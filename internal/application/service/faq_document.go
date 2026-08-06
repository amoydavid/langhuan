package service

import (
	"context"
	"encoding/json"
	"fmt"
	id "github.com/dajee/langhuan/internal/domain/id"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/ports/queue"
)

const faqDocumentIndexJobType = "document_index"

// FAQDocumentServiceDeps contains FAQ aggregate persistence and async dispatch.
type FAQDocumentServiceDeps struct {
	Store FAQRevisionStore
	Queue queue.JobQueue
}

// FAQDocumentService manages complete FAQ revisions.
type FAQDocumentService struct {
	store FAQRevisionStore
	queue queue.JobQueue
}

// CreateFAQDocumentInput is a complete first FAQ revision.
type CreateFAQDocumentInput struct {
	WorkspaceID     uuid.UUID
	KnowledgeBaseID uuid.UUID
	Title           string
	Questions       []string
	Answer          string
	CreatedBy       *uuid.UUID
}

// UpdateFAQDocumentInput is a complete replacement guarded by the active revision.
type UpdateFAQDocumentInput struct {
	WorkspaceID     uuid.UUID
	KnowledgeBaseID uuid.UUID
	DocumentID      uuid.UUID
	BaseRevisionID  uuid.UUID
	Questions       []string
	Answer          string
	CreatedBy       *uuid.UUID
}

// NewFAQDocumentService creates an FAQ document service.
func NewFAQDocumentService(deps FAQDocumentServiceDeps) *FAQDocumentService {
	return &FAQDocumentService{store: deps.Store, queue: deps.Queue}
}

// Create atomically writes Document, ready FAQ Revision, questions, answer and index Job.
func (s *FAQDocumentService) Create(ctx context.Context, input CreateFAQDocumentInput) (*dto.FAQDocument, error) {
	if input.WorkspaceID == uuid.Nil || input.KnowledgeBaseID == uuid.Nil {
		return nil, fmt.Errorf("%w: Workspace/KnowledgeBase ID 不能为空", domainerrors.ErrValidation)
	}
	document, err := model.NewDocumentIdentity(
		input.WorkspaceID, input.KnowledgeBaseID, value.DocumentKindFAQ,
		input.Title, "api", "", map[string]any{},
	)
	if err != nil {
		return nil, err
	}
	revision, faq, err := newFAQReadyRevision(
		document, 1, value.DocumentRevisionReasonIngest, input.Questions, input.Answer, input.CreatedBy,
	)
	if err != nil {
		return nil, err
	}
	var job *model.Job
	err = s.store.WithinWorkspace(ctx, input.WorkspaceID, func(txCtx context.Context, tx FAQRevisionTx) error {
		knowledgeBase, err := tx.GetKnowledgeBase(txCtx, input.KnowledgeBaseID)
		if err != nil {
			return err
		}
		job, err = newFAQIndexJob(document, revision, knowledgeBase)
		if err != nil {
			return err
		}
		return tx.CreateFAQRevisionAggregate(txCtx, document, faq, job)
	})
	if err != nil {
		return nil, err
	}
	if err := s.enqueueIndex(ctx, job); err != nil {
		return nil, err
	}
	return dto.FAQDocumentFromModel(document, faq, job), nil
}

// Update appends one complete FAQ revision without switching the active Document pointer.
func (s *FAQDocumentService) Update(ctx context.Context, input UpdateFAQDocumentInput) (*dto.FAQDocument, error) {
	if input.WorkspaceID == uuid.Nil || input.DocumentID == uuid.Nil ||
		input.BaseRevisionID == uuid.Nil {
		return nil, fmt.Errorf("%w: FAQ update lineage/base 不能为空", domainerrors.ErrValidation)
	}
	if err := validateFAQPayload(input.Questions, input.Answer); err != nil {
		return nil, err
	}
	var document *model.Document
	var faq *model.FAQRevision
	var job *model.Job
	var err error
	err = s.store.WithinWorkspace(ctx, input.WorkspaceID, func(txCtx context.Context, tx FAQRevisionTx) error {
		document, err = tx.GetDocumentForUpdate(txCtx, input.DocumentID)
		if err != nil {
			return err
		}
		if (input.KnowledgeBaseID != uuid.Nil && document.KnowledgeBaseID != input.KnowledgeBaseID) ||
			document.Kind != value.DocumentKindFAQ {
			return domainerrors.ErrNotFound
		}
		knowledgeBase, err := tx.GetKnowledgeBase(txCtx, document.KnowledgeBaseID)
		if err != nil {
			return err
		}
		if document.ActiveRevisionID == nil || *document.ActiveRevisionID != input.BaseRevisionID {
			return domainerrors.ErrRevisionConflict
		}
		base, err := tx.GetFAQRevision(txCtx, input.BaseRevisionID)
		if err != nil {
			return err
		}
		if base.DocumentRevision.DocumentID != document.ID {
			return domainerrors.ErrRevisionConflict
		}
		_, faq, err = newFAQReadyRevision(
			document, base.DocumentRevision.RevisionNo+1, value.DocumentRevisionReasonEdit,
			input.Questions, input.Answer, input.CreatedBy,
		)
		if err != nil {
			return err
		}
		document.Status = value.DocumentStatusPending
		document.UpdatedAt = time.Now().UTC()
		job, err = newFAQIndexJob(document, faq.DocumentRevision, knowledgeBase)
		if err != nil {
			return err
		}
		return tx.CreateFAQRevisionAggregate(txCtx, document, faq, job)
	})
	if err != nil {
		return nil, err
	}
	if err := s.enqueueIndex(ctx, job); err != nil {
		return nil, err
	}
	return dto.FAQDocumentFromModel(document, faq, job), nil
}

// Get returns the currently active complete FAQ revision.
func (s *FAQDocumentService) Get(
	ctx context.Context,
	workspaceID, knowledgeBaseID, documentID uuid.UUID,
) (*dto.FAQDocument, error) {
	if workspaceID == uuid.Nil || knowledgeBaseID == uuid.Nil || documentID == uuid.Nil {
		return nil, fmt.Errorf("%w: FAQ lineage 无效", domainerrors.ErrValidation)
	}
	var document *model.Document
	var faq *model.FAQRevision
	err := s.store.WithinWorkspace(ctx, workspaceID, func(txCtx context.Context, tx FAQRevisionTx) error {
		var err error
		document, err = tx.GetDocumentForUpdate(txCtx, documentID)
		if err != nil {
			return err
		}
		if document.Kind != value.DocumentKindFAQ || document.ActiveRevisionID == nil {
			return domainerrors.ErrNotFound
		}
		if document.KnowledgeBaseID != knowledgeBaseID {
			return domainerrors.ErrNotFound
		}
		faq, err = tx.GetFAQRevision(txCtx, *document.ActiveRevisionID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return dto.FAQDocumentFromModel(document, faq, nil), nil
}

func newFAQReadyRevision(
	document *model.Document,
	revisionNo int64,
	reason value.DocumentRevisionReason,
	questions []string,
	answer string,
	createdBy *uuid.UUID,
) (*model.DocumentRevision, *model.FAQRevision, error) {
	revision, err := model.NewDocumentRevision(model.NewDocumentRevisionInput{
		WorkspaceID: document.WorkspaceID, KnowledgeBaseID: document.KnowledgeBaseID,
		DocumentID: document.ID, Kind: value.DocumentKindFAQ, DocumentKind: document.Kind,
		RevisionNo: revisionNo, Reason: reason, ProcessingVersion: model.CurrentProcessingVersion,
		Status: value.DocumentRevisionReady, CreatedBy: createdBy,
	})
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	revision.CompletedAt = &now
	faq, err := model.NewFAQRevision(model.NewFAQRevisionInput{
		DocumentRevision: revision, Answer: answer, Questions: questions,
	})
	if err != nil {
		return nil, nil, err
	}
	return revision, faq, nil
}

func validateFAQPayload(questions []string, answer string) error {
	document := &model.Document{
		ID: id.New(), WorkspaceID: id.New(), KnowledgeBaseID: id.New(), Kind: value.DocumentKindFAQ,
	}
	_, _, err := newFAQReadyRevision(
		document, 1, value.DocumentRevisionReasonEdit, questions, answer, nil,
	)
	return err
}

func newFAQIndexJob(
	document *model.Document,
	revision *model.DocumentRevision,
	knowledgeBase *model.KnowledgeBase,
) (*model.Job, error) {
	if knowledgeBase == nil || knowledgeBase.ID != document.KnowledgeBaseID ||
		knowledgeBase.WorkspaceID != document.WorkspaceID || knowledgeBase.ActiveIndexGenerationID == nil {
		return nil, fmt.Errorf("%w: FAQ KnowledgeBase/Generation lineage 无效", domainerrors.ErrValidation)
	}
	return model.NewJob(model.NewJobInput{
		WorkspaceID: document.WorkspaceID, KnowledgeBaseID: document.KnowledgeBaseID,
		DocumentID: document.ID, DocumentRevisionID: revision.ID,
		Type: faqDocumentIndexJobType, Status: value.JobStatusPending,
		Payload: map[string]any{
			"workspace_id": document.WorkspaceID.String(), "knowledge_base_id": document.KnowledgeBaseID.String(),
			"document_id": document.ID.String(), "document_revision_id": revision.ID.String(),
			"index_generation_id": knowledgeBase.ActiveIndexGenerationID.String(),
		},
	})
}

func (s *FAQDocumentService) enqueueIndex(ctx context.Context, job *model.Job) error {
	if s.queue == nil {
		return fmt.Errorf("FAQ index enqueue failed: queue is nil")
	}
	rawGenerationID, ok := job.Payload["index_generation_id"].(string)
	if !ok {
		return fmt.Errorf("%w: FAQ Job index_generation_id 无效", domainerrors.ErrValidation)
	}
	generationID, err := uuid.Parse(strings.TrimSpace(rawGenerationID))
	if err != nil || generationID == uuid.Nil {
		return fmt.Errorf("%w: FAQ Job index_generation_id 无效", domainerrors.ErrValidation)
	}
	payload := map[string]string{
		"workspace_id": job.WorkspaceID.String(), "knowledge_base_id": job.KnowledgeBaseID.String(),
		"document_id": job.DocumentID.String(), "document_revision_id": job.DocumentRevisionID.String(),
		"generation_id": generationID.String(), "job_id": job.ID.String(),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("编码 FAQ index Job 失败: %w", err)
	}
	if _, err := s.queue.Enqueue(ctx, queue.JobRequest{
		Type: faqDocumentIndexJobType, Payload: encoded,
		TaskID: queue.DocumentTaskID(faqDocumentIndexJobType, job.WorkspaceID, job.DocumentRevisionID, generationID),
	}); err != nil {
		return fmt.Errorf("入队 FAQ index Job 失败: %w", err)
	}
	return nil
}
