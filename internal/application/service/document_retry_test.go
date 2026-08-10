package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/ports/queue"
)

// fakeRetryQueue 记录入队请求。
type fakeRetryQueue struct {
	requests []queue.JobRequest
	err      error
}

func (q *fakeRetryQueue) Enqueue(_ context.Context, req queue.JobRequest) (*queue.JobHandle, error) {
	if q.err != nil {
		return nil, q.err
	}
	q.requests = append(q.requests, req)
	return &queue.JobHandle{ID: "queued"}, nil
}

// fakeRetryTx 是 DocumentRetryTx 的可控 mock。
type fakeRetryTx struct {
	kb             *model.KnowledgeBase
	latestRevision *model.DocumentRevision
	jobRevision    *JobRevision
	resetJobID     uuid.UUID
	resetErr       error
	latestErr      error
	failResetCalls int
}

func (tx *fakeRetryTx) GetKnowledgeBase(_ context.Context, _ uuid.UUID) (*model.KnowledgeBase, error) {
	return tx.kb, nil
}
func (tx *fakeRetryTx) GetLatestRevision(_ context.Context, _ uuid.UUID) (*model.DocumentRevision, error) {
	if tx.latestErr != nil {
		return nil, tx.latestErr
	}
	return tx.latestRevision, nil
}
func (tx *fakeRetryTx) GetJobRevision(_ context.Context, _ uuid.UUID) (*JobRevision, error) {
	return tx.jobRevision, nil
}
func (tx *fakeRetryTx) ResetFailedRevision(_ context.Context, _ ResetFailedRevisionRequest) (uuid.UUID, error) {
	return tx.resetJobID, tx.resetErr
}
func (tx *fakeRetryTx) FailReset(_ context.Context, _ ResetFailedRevisionRequest, _ uuid.UUID, _ string) error {
	tx.failResetCalls++
	return nil
}

// fakeRetryStore 进入事务并执行回调。
type fakeRetryStore struct {
	tx DocumentRetryTx
}

func (s *fakeRetryStore) WithinWorkspace(_ context.Context, _ uuid.UUID, fn func(context.Context, DocumentRetryTx) error) error {
	return fn(context.Background(), s.tx)
}

func newRetryService(tx DocumentRetryTx, q *fakeRetryQueue) *DocumentRetryService {
	return NewDocumentRetryService(DocumentRetryServiceDeps{
		Store:  &fakeRetryStore{tx: tx},
		Queue:  q,
		Logger: nil,
	})
}

func unrestrictedAccess(ws uuid.UUID) value.ResourceAccess {
	return value.ResourceAccess{WorkspaceID: ws, Unrestricted: true}
}

func TestRetryDocumentSuccess(t *testing.T) {
	ws, kb, doc, rev, gen, job := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	genPtr := gen
	tx := &fakeRetryTx{
		kb:             &model.KnowledgeBase{ID: kb, WorkspaceID: ws, ActiveIndexGenerationID: &genPtr},
		latestRevision: &model.DocumentRevision{ID: rev, WorkspaceID: ws, KnowledgeBaseID: kb, DocumentID: doc},
		resetJobID:     job,
	}
	q := &fakeRetryQueue{}
	svc := newRetryService(tx, q)

	result, err := svc.RetryDocument(context.Background(), unrestrictedAccess(ws), doc)
	if err != nil {
		t.Fatalf("RetryDocument error = %v", err)
	}
	if result.JobID != job || result.RevisionID != rev || result.DocumentID != doc {
		t.Fatalf("result = %#v", result)
	}
	// 验证入队了 document_parse_start，且 TaskID 含 workspace+revision+generation。
	if len(q.requests) != 1 {
		t.Fatalf("enqueue count = %d, want 1", len(q.requests))
	}
	req := q.requests[0]
	if req.Type != documentParseStartJobType {
		t.Fatalf("enqueued type = %s, want %s", req.Type, documentParseStartJobType)
	}
	wantTaskID := queue.DocumentTaskID(documentParseStartJobType, ws, rev, gen)
	if req.TaskID != wantTaskID {
		t.Fatalf("TaskID = %s, want %s", req.TaskID, wantTaskID)
	}
}

func TestRetryDocumentRejectsNotRetryable(t *testing.T) {
	ws, kb, doc, rev, gen := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	genPtr := gen
	tx := &fakeRetryTx{
		kb:             &model.KnowledgeBase{ID: kb, WorkspaceID: ws, ActiveIndexGenerationID: &genPtr},
		latestRevision: &model.DocumentRevision{ID: rev, WorkspaceID: ws, KnowledgeBaseID: kb, DocumentID: doc},
		resetErr:       domainerrors.ErrNotRetryable,
	}
	q := &fakeRetryQueue{}
	svc := newRetryService(tx, q)

	_, err := svc.RetryDocument(context.Background(), unrestrictedAccess(ws), doc)
	if !errors.Is(err, domainerrors.ErrNotRetryable) {
		t.Fatalf("error = %v, want ErrNotRetryable", err)
	}
	// 复位失败时不应入队。
	if len(q.requests) != 0 {
		t.Fatalf("enqueue count = %d, want 0 on reset failure", len(q.requests))
	}
}

func TestRetryDocumentMissingActiveGeneration(t *testing.T) {
	ws, kb, doc, rev := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	tx := &fakeRetryTx{
		kb:             &model.KnowledgeBase{ID: kb, WorkspaceID: ws, ActiveIndexGenerationID: nil},
		latestRevision: &model.DocumentRevision{ID: rev, WorkspaceID: ws, KnowledgeBaseID: kb, DocumentID: doc},
	}
	q := &fakeRetryQueue{}
	svc := newRetryService(tx, q)

	_, err := svc.RetryDocument(context.Background(), unrestrictedAccess(ws), doc)
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
}

func TestRetryDocumentAPIKeyCrossKBNotFound(t *testing.T) {
	ws, kb, doc, rev, otherKB := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	tx := &fakeRetryTx{
		latestRevision: &model.DocumentRevision{ID: rev, WorkspaceID: ws, KnowledgeBaseID: otherKB, DocumentID: doc},
	}
	q := &fakeRetryQueue{}
	svc := newRetryService(tx, q)

	// 受限 access 只允许 kb，但 revision 属于 otherKB → 应 404。
	access := value.ResourceAccess{
		WorkspaceID: ws, Unrestricted: false, AllowedKnowledgeBaseIDs: []uuid.UUID{kb},
	}
	_, err := svc.RetryDocument(context.Background(), access, doc)
	if !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	// kb 变量在这里只用于 access 构造，避免 unused 警告。
	_ = kb
}

// TestRetryDocumentEnqueueFailureTriggersFailReset 验证入队失败时调用 FailReset 补偿
// （revision/job 标回 failed），避免"revision 已 pending 但任务未入队"的永久卡死。
func TestRetryDocumentEnqueueFailureTriggersFailReset(t *testing.T) {
	ws, kb, doc, rev, gen, job := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	genPtr := gen
	tx := &fakeRetryTx{
		kb:             &model.KnowledgeBase{ID: kb, WorkspaceID: ws, ActiveIndexGenerationID: &genPtr},
		latestRevision: &model.DocumentRevision{ID: rev, WorkspaceID: ws, KnowledgeBaseID: kb, DocumentID: doc},
		resetJobID:     job,
	}
	q := &fakeRetryQueue{err: fmt.Errorf("redis 不可用")}
	svc := newRetryService(tx, q)

	_, err := svc.RetryDocument(context.Background(), unrestrictedAccess(ws), doc)
	if err == nil {
		t.Fatal("error = nil, want enqueue error")
	}
	if tx.failResetCalls != 1 {
		t.Fatalf("FailReset calls = %d, want 1", tx.failResetCalls)
	}
}

func TestRetryJobSuccess(t *testing.T) {
	ws, kb, doc, rev, gen, jobID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	genPtr := gen
	tx := &fakeRetryTx{
		kb:          &model.KnowledgeBase{ID: kb, WorkspaceID: ws, ActiveIndexGenerationID: &genPtr},
		jobRevision: &JobRevision{JobID: jobID, KnowledgeBaseID: kb, DocumentID: doc, RevisionID: rev},
		resetJobID:  uuid.New(),
	}
	q := &fakeRetryQueue{}
	svc := newRetryService(tx, q)

	result, err := svc.RetryJob(context.Background(), unrestrictedAccess(ws), jobID)
	if err != nil {
		t.Fatalf("RetryJob error = %v", err)
	}
	if result.RevisionID != rev || result.DocumentID != doc {
		t.Fatalf("result = %#v", result)
	}
	if len(q.requests) != 1 {
		t.Fatalf("enqueue count = %d, want 1", len(q.requests))
	}
}

func TestRetryDocumentEmptyInput(t *testing.T) {
	svc := newRetryService(&fakeRetryTx{}, &fakeRetryQueue{})
	_, err := svc.RetryDocument(context.Background(), value.ResourceAccess{}, uuid.Nil)
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
}

func TestRetryJobEmptyInput(t *testing.T) {
	svc := newRetryService(&fakeRetryTx{}, &fakeRetryQueue{})
	_, err := svc.RetryJob(context.Background(), value.ResourceAccess{}, uuid.Nil)
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
}
