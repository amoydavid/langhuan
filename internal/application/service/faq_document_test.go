package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/ports/queue"
)

func TestCreateFAQWritesCompleteRevisionAndQueuesAfterCommit(t *testing.T) {
	workspaceID, knowledgeBaseID, generationID := uuid.New(), uuid.New(), uuid.New()
	store := newFakeFAQRevisionStore(&model.KnowledgeBase{
		ID: knowledgeBaseID, WorkspaceID: workspaceID, ActiveIndexGenerationID: &generationID,
	})
	jobQueue := &faqTestQueue{store: store}
	service := NewFAQDocumentService(FAQDocumentServiceDeps{Store: store, Queue: jobQueue})

	got, err := service.Create(context.Background(), CreateFAQDocumentInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID, Title: "退款",
		Questions: []string{"如何退款？", "退款流程是什么？"}, Answer: "请在订单页申请退款。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision.RevisionNo != 1 || len(got.Questions) != 2 || got.Answer != "请在订单页申请退款。" {
		t.Fatalf("created FAQ = %#v", got)
	}
	if store.savedFAQ == nil || store.savedJob == nil || store.savedDocument == nil {
		t.Fatalf("saved aggregate = %#v %#v %#v", store.savedDocument, store.savedFAQ, store.savedJob)
	}
	if store.savedFAQ.DocumentRevision.Status != value.DocumentRevisionReady || store.savedFAQ.DocumentRevision.CompletedAt == nil {
		t.Fatalf("revision = %#v", store.savedFAQ.DocumentRevision)
	}
	if store.savedDocument.ActiveRevisionID != nil {
		t.Fatalf("active revision = %v, want nil before publish", store.savedDocument.ActiveRevisionID)
	}
	if store.savedJob.DocumentRevisionID != store.savedFAQ.DocumentRevision.ID || store.savedJob.IndexGenerationID != uuid.Nil ||
		store.savedJob.Payload["index_generation_id"] != generationID.String() {
		t.Fatalf("job lineage = %#v", store.savedJob)
	}
	if len(jobQueue.requests) != 1 || !jobQueue.committedWhenEnqueued {
		t.Fatalf("queue requests=%d committed=%v", len(jobQueue.requests), jobQueue.committedWhenEnqueued)
	}
	var queuePayload map[string]string
	if err := json.Unmarshal(jobQueue.requests[0].Payload, &queuePayload); err != nil {
		t.Fatal(err)
	}
	if queuePayload["generation_id"] != generationID.String() || queuePayload["index_generation_id"] != "" {
		t.Fatalf("queue payload = %#v", queuePayload)
	}
	wantTaskID := queue.DocumentTaskID(
		faqDocumentIndexJobType, workspaceID, store.savedFAQ.DocumentRevision.ID, generationID,
	)
	if jobQueue.requests[0].TaskID != wantTaskID {
		t.Fatalf("queue task id = %q, want %q", jobQueue.requests[0].TaskID, wantTaskID)
	}
}

func TestCreateFAQRejectsInvalidAggregateBeforeStore(t *testing.T) {
	tests := []struct {
		name      string
		questions []string
		answer    string
	}{
		{name: "empty answer", questions: []string{"问题"}},
		{name: "zero questions", answer: "回答"},
		{name: "whitespace question", questions: []string{"\u3000\t"}, answer: "回答"},
		{name: "duplicate normalized", questions: []string{" How   To Refund? ", "how to refund?"}, answer: "回答"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeFAQRevisionStore(&model.KnowledgeBase{
				ID: uuid.New(), WorkspaceID: uuid.New(), ActiveIndexGenerationID: pointerUUID(uuid.New()),
			})
			service := NewFAQDocumentService(FAQDocumentServiceDeps{Store: store, Queue: &faqTestQueue{store: store}})
			_, err := service.Create(context.Background(), CreateFAQDocumentInput{
				WorkspaceID: store.kb.WorkspaceID, KnowledgeBaseID: store.kb.ID,
				Title: "FAQ", Questions: test.questions, Answer: test.answer,
			})
			if !errors.Is(err, domainerrors.ErrValidation) {
				t.Fatalf("error = %v, want ErrValidation", err)
			}
			if store.withinCalls != 0 || store.saveCalls != 0 {
				t.Fatalf("store calls: within=%d save=%d", store.withinCalls, store.saveCalls)
			}
		})
	}
}

func TestUpdateFAQCreatesCompleteRevision(t *testing.T) {
	workspaceID, knowledgeBaseID, generationID := uuid.New(), uuid.New(), uuid.New()
	documentID, activeRevisionID := uuid.New(), uuid.New()
	document := &model.Document{
		ID: documentID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
		Kind: value.DocumentKindFAQ, Title: "退款", ActiveRevisionID: &activeRevisionID,
	}
	active := fakeFAQAggregate(workspaceID, knowledgeBaseID, documentID, activeRevisionID, 1)
	store := newFakeFAQRevisionStore(&model.KnowledgeBase{
		ID: knowledgeBaseID, WorkspaceID: workspaceID, ActiveIndexGenerationID: &generationID,
	})
	store.document = document
	store.faqs[activeRevisionID] = active
	service := NewFAQDocumentService(FAQDocumentServiceDeps{Store: store, Queue: &faqTestQueue{store: store}})

	got, err := service.Update(context.Background(), UpdateFAQDocumentInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID, DocumentID: documentID,
		BaseRevisionID: activeRevisionID,
		Questions:      []string{"如何退款？", "退款流程是什么？"}, Answer: "请在订单页申请退款。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision.RevisionNo != 2 || len(got.Questions) != 2 || store.savedFAQ.DocumentRevision.Reason != value.DocumentRevisionReasonEdit {
		t.Fatalf("updated FAQ = %#v", got)
	}
	if document.ActiveRevisionID == nil || *document.ActiveRevisionID != activeRevisionID {
		t.Fatalf("active revision changed before publish: %v", document.ActiveRevisionID)
	}
}

func TestUpdateFAQRejectsStaleBaseWithoutWrite(t *testing.T) {
	workspaceID, knowledgeBaseID := uuid.New(), uuid.New()
	documentID, activeRevisionID := uuid.New(), uuid.New()
	store := newFakeFAQRevisionStore(&model.KnowledgeBase{
		ID: knowledgeBaseID, WorkspaceID: workspaceID, ActiveIndexGenerationID: pointerUUID(uuid.New()),
	})
	store.document = &model.Document{
		ID: documentID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
		Kind: value.DocumentKindFAQ, ActiveRevisionID: &activeRevisionID,
	}
	service := NewFAQDocumentService(FAQDocumentServiceDeps{Store: store, Queue: &faqTestQueue{store: store}})

	_, err := service.Update(context.Background(), UpdateFAQDocumentInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID, DocumentID: documentID,
		BaseRevisionID: uuid.New(), Questions: []string{"问题"}, Answer: "回答",
	})
	if !errors.Is(err, domainerrors.ErrRevisionConflict) {
		t.Fatalf("error = %v, want ErrRevisionConflict", err)
	}
	if store.saveCalls != 0 {
		t.Fatalf("save calls = %d, want 0", store.saveCalls)
	}
}

type fakeFAQRevisionStore struct {
	kb            *model.KnowledgeBase
	document      *model.Document
	faqs          map[uuid.UUID]*model.FAQRevision
	savedDocument *model.Document
	savedFAQ      *model.FAQRevision
	savedJob      *model.Job
	withinCalls   int
	saveCalls     int
	committed     bool
}

func newFakeFAQRevisionStore(kb *model.KnowledgeBase) *fakeFAQRevisionStore {
	return &fakeFAQRevisionStore{kb: kb, faqs: map[uuid.UUID]*model.FAQRevision{}}
}

func (s *fakeFAQRevisionStore) WithinWorkspace(
	ctx context.Context,
	workspaceID uuid.UUID,
	fn func(context.Context, FAQRevisionTx) error,
) error {
	s.withinCalls++
	if s.kb == nil || s.kb.WorkspaceID != workspaceID {
		return domainerrors.ErrNotFound
	}
	err := fn(ctx, s)
	if err == nil {
		s.committed = true
	}
	return err
}

func (s *fakeFAQRevisionStore) GetKnowledgeBase(_ context.Context, id uuid.UUID) (*model.KnowledgeBase, error) {
	if s.kb == nil || s.kb.ID != id {
		return nil, domainerrors.ErrNotFound
	}
	return s.kb, nil
}

func (s *fakeFAQRevisionStore) GetDocumentForUpdate(_ context.Context, id uuid.UUID) (*model.Document, error) {
	if s.document == nil || s.document.ID != id {
		return nil, domainerrors.ErrNotFound
	}
	return s.document, nil
}

func (s *fakeFAQRevisionStore) GetFAQRevision(_ context.Context, id uuid.UUID) (*model.FAQRevision, error) {
	faq, ok := s.faqs[id]
	if !ok {
		return nil, domainerrors.ErrNotFound
	}
	return faq, nil
}

func (s *fakeFAQRevisionStore) CreateFAQRevisionAggregate(
	_ context.Context,
	document *model.Document,
	faq *model.FAQRevision,
	job *model.Job,
) error {
	s.saveCalls++
	s.savedDocument, s.savedFAQ, s.savedJob = document, faq, job
	s.document = document
	s.faqs[faq.DocumentRevision.ID] = faq
	return nil
}

type faqTestQueue struct {
	store                 *fakeFAQRevisionStore
	requests              []queue.JobRequest
	committedWhenEnqueued bool
}

func (q *faqTestQueue) Enqueue(_ context.Context, request queue.JobRequest) (*queue.JobHandle, error) {
	q.committedWhenEnqueued = q.store.committed
	q.requests = append(q.requests, request)
	return &queue.JobHandle{ID: uuid.NewString()}, nil
}

func fakeFAQAggregate(workspaceID, knowledgeBaseID, documentID, revisionID uuid.UUID, revisionNo int64) *model.FAQRevision {
	revision := &model.DocumentRevision{
		ID: revisionID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
		DocumentID: documentID, Kind: value.DocumentKindFAQ, RevisionNo: revisionNo,
		Status: value.DocumentRevisionReady,
	}
	return &model.FAQRevision{DocumentRevision: revision, Answer: "旧回答", Questions: []model.FAQRevisionQuestion{{
		ID: uuid.New(), WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
		DocumentID: documentID, DocumentRevisionID: revisionID, Sequence: 0,
		Question: "旧问题", NormalizedQuestion: "旧问题",
	}}}
}

func pointerUUID(id uuid.UUID) *uuid.UUID { return &id }
