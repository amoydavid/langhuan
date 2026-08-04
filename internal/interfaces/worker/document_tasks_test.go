package worker

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	parserport "github.com/dajee/langhuan/internal/ports/parser"
	"github.com/dajee/langhuan/internal/ports/queue"
)

func TestDocumentTaskRequiresWorkspaceAndRevision(t *testing.T) {
	tests := []struct {
		name    string
		payload DocumentTaskPayload
		want    string
	}{
		{
			name: "missing workspace",
			payload: DocumentTaskPayload{
				KnowledgeBaseID: uuid.New(), DocumentID: uuid.New(),
				DocumentRevisionID: uuid.New(), GenerationID: uuid.New(), JobID: uuid.New(),
			},
			want: "workspace_id",
		},
		{
			name: "missing revision",
			payload: DocumentTaskPayload{
				WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(),
				DocumentID: uuid.New(), GenerationID: uuid.New(), JobID: uuid.New(),
			},
			want: "document_revision_id",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newDocumentTaskFixture(value.DocumentKindFile, value.DocumentRevisionPending)
			err := processDocumentTask(context.Background(), TaskDocumentParseStart, fixture.handlers(), tt.payload)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %s validation", err, tt.want)
			}
			if fixture.store.withinCalls != 0 || len(fixture.store.marks) != 0 {
				t.Fatalf("repositories called before validation: within=%d marks=%#v", fixture.store.withinCalls, fixture.store.marks)
			}
		})
	}
}

func TestDocumentTaskRejectsJobLineageMismatch(t *testing.T) {
	fixture := newDocumentTaskFixture(value.DocumentKindFile, value.DocumentRevisionPending)
	payload := fixture.payload()
	fixture.store.job.KnowledgeBaseID = uuid.New()

	err := processDocumentTask(context.Background(), TaskDocumentParseStart, fixture.handlers(), payload)
	if err == nil || !strings.Contains(err.Error(), "lineage") {
		t.Fatalf("error = %v, want lineage mismatch", err)
	}
	if len(fixture.store.marks) != 0 || len(fixture.queue.requests) != 0 {
		t.Fatalf("side effects before lineage validation: marks=%#v queue=%#v", fixture.store.marks, fixture.queue.requests)
	}
}

func TestDocumentParsePollIsKindAwareAndIdempotent(t *testing.T) {
	tests := []struct {
		name      string
		kind      value.DocumentKind
		status    value.DocumentRevisionStatus
		wantParse int
	}{
		{name: "pending file parses", kind: value.DocumentKindFile, status: value.DocumentRevisionPending, wantParse: 1},
		{name: "pending web parses", kind: value.DocumentKindWeb, status: value.DocumentRevisionPending, wantParse: 1},
		{name: "ready file skips parse", kind: value.DocumentKindFile, status: value.DocumentRevisionReady},
		{name: "faq never parses", kind: value.DocumentKindFAQ, status: value.DocumentRevisionReady},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newDocumentTaskFixture(tt.kind, tt.status)
			payload := fixture.payload()
			fixture.store.job.Type = TaskDocumentParsePoll

			err := processDocumentTask(context.Background(), TaskDocumentParsePoll, fixture.handlers(), payload)
			if err != nil {
				t.Fatal(err)
			}
			if len(fixture.pipeline.parseCalls) != tt.wantParse {
				t.Fatalf("parse calls = %#v, want %d", fixture.pipeline.parseCalls, tt.wantParse)
			}
			if len(fixture.queue.requests) != 1 || fixture.queue.requests[0].Type != TaskDocumentIndex {
				t.Fatalf("queue requests = %#v, want one index task", fixture.queue.requests)
			}
			assertQueuedLineage(t, fixture.queue.requests[0], payload, uuid.Nil)
		})
	}
}

func TestDocumentIndexBuildsChunkSetThenPublishes(t *testing.T) {
	fixture := newDocumentTaskFixture(value.DocumentKindFAQ, value.DocumentRevisionReady)
	payload := fixture.payload()
	fixture.store.job.Type = TaskDocumentIndex
	fixture.pipeline.chunkSetID = uuid.New()

	err := processDocumentTask(context.Background(), TaskDocumentIndex, fixture.handlers(), payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(fixture.pipeline.chunkCalls) != 1 {
		t.Fatalf("chunk calls = %#v, want one", fixture.pipeline.chunkCalls)
	}
	if len(fixture.pipeline.indexCalls) != 1 {
		t.Fatalf("index calls = %#v, want one", fixture.pipeline.indexCalls)
	}
	indexCall := fixture.pipeline.indexCalls[0]
	if indexCall.workspaceID != payload.WorkspaceID || indexCall.generationID != payload.GenerationID ||
		indexCall.chunkSetID != fixture.pipeline.chunkSetID {
		t.Fatalf("index call = %#v", indexCall)
	}
}

func TestDocumentIndexReusesPayloadChunkSet(t *testing.T) {
	fixture := newDocumentTaskFixture(value.DocumentKindFile, value.DocumentRevisionReady)
	payload := fixture.payload()
	payload.ChunkSetID = uuid.New()
	fixture.store.job.Type = TaskDocumentIndex

	err := processDocumentTask(context.Background(), TaskDocumentIndex, fixture.handlers(), payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(fixture.pipeline.chunkCalls) != 0 {
		t.Fatalf("chunk calls = %#v, want none", fixture.pipeline.chunkCalls)
	}
	if len(fixture.pipeline.indexCalls) != 1 || fixture.pipeline.indexCalls[0].chunkSetID != payload.ChunkSetID {
		t.Fatalf("index calls = %#v", fixture.pipeline.indexCalls)
	}
}

func TestDocumentIndexSkipsPublishedRevision(t *testing.T) {
	fixture := newDocumentTaskFixture(value.DocumentKindFile, value.DocumentRevisionReady)
	payload := fixture.payload()
	fixture.store.job.Type = TaskDocumentIndex
	fixture.store.published = true

	err := processDocumentTask(context.Background(), TaskDocumentIndex, fixture.handlers(), payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(fixture.pipeline.chunkCalls) != 0 || len(fixture.pipeline.indexCalls) != 0 {
		t.Fatalf("pipeline called for published revision: chunk=%#v index=%#v", fixture.pipeline.chunkCalls, fixture.pipeline.indexCalls)
	}
	assertJobMarks(t, fixture.store.marks, value.JobStatusRunning, value.JobStatusCompleted)
}

func TestPermanentDocumentTaskErrorPersistsFailureBeforeSkipRetry(t *testing.T) {
	fixture := newDocumentTaskFixture(value.DocumentKindFile, value.DocumentRevisionPending)
	payload := fixture.payload()
	fixture.store.job.Type = TaskDocumentParsePoll
	fixture.pipeline.parseErr = parserport.ErrInvalidDocument

	err := processDocumentTask(context.Background(), TaskDocumentParsePoll, fixture.handlers(), payload)
	if !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("error = %v, want SkipRetry", err)
	}
	if len(fixture.store.failures) != 1 {
		t.Fatalf("failures = %#v, want one durable failure", fixture.store.failures)
	}
}

func TestPermanentDocumentTaskErrorRemainsRetryableWhenFailurePersistenceFails(t *testing.T) {
	fixture := newDocumentTaskFixture(value.DocumentKindFile, value.DocumentRevisionPending)
	payload := fixture.payload()
	fixture.store.job.Type = TaskDocumentParsePoll
	fixture.pipeline.parseErr = parserport.ErrInvalidDocument
	fixture.store.failErr = errors.New("database unavailable")

	err := processDocumentTask(context.Background(), TaskDocumentParsePoll, fixture.handlers(), payload)
	if err == nil || errors.Is(err, asynq.SkipRetry) || !errors.Is(err, fixture.store.failErr) {
		t.Fatalf("error = %v, want retryable persistence error", err)
	}
}

func TestDocumentTaskQueueUsesCompleteLineageAndDeterministicTaskID(t *testing.T) {
	fixture := newDocumentTaskFixture(value.DocumentKindFile, value.DocumentRevisionPending)
	payload := fixture.payload()

	err := processDocumentTask(context.Background(), TaskDocumentParseStart, fixture.handlers(), payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(fixture.queue.requests) != 1 {
		t.Fatalf("queue requests = %#v", fixture.queue.requests)
	}
	request := fixture.queue.requests[0]
	if request.Type != TaskDocumentParsePoll {
		t.Fatalf("queue type = %q", request.Type)
	}
	// poll 任务使用 jobID 维度的唯一 TaskID（异步 poll 会多次重入队，不能复用固定 ID）
	wantTaskID := queue.DocumentPollTaskID(payload.WorkspaceID, payload.DocumentRevisionID, fixture.store.created[0].ID)
	if request.TaskID != wantTaskID {
		t.Fatalf("task id = %q, want %q", request.TaskID, wantTaskID)
	}
	assertQueuedLineage(t, request, payload, uuid.Nil)
}

// TestDocumentParsePollRequeuesWithUniqueTaskID 验证异步 poll 重入队使用唯一 TaskID，
// 避免 asynq "task ID conflicts with another task"。
func TestDocumentParsePollRequeuesWithUniqueTaskID(t *testing.T) {
	asyncParser := &fakeAsyncParser{
		pollResult: &parserport.AsyncParsePollResult{
			Status:  parserport.AsyncRunning,
			Payload: map[string]any{"poll_count": 1},
		},
	}
	fixture := newPDFPipelineFixture(asyncParser, parserport.AsyncRunning)
	payload := fixture.payload()

	fixture.store.revision.FileType = "pdf"
	fixture.store.revision.Status = value.DocumentRevisionPending
	fixture.store.job.Type = TaskDocumentParsePoll
	fixture.store.job.Payload["external_job_id"] = "fake-mineru-batch-001"

	err := processDocumentTask(context.Background(), TaskDocumentParsePoll, fixture.handlers(), payload)
	if err != nil {
		t.Fatalf("parse_poll error = %v", err)
	}

	// 重入队的 poll 任务应有唯一 TaskID（含新 job ID）
	if len(fixture.queue.requests) == 0 {
		t.Fatal("no poll task was re-enqueued")
	}
	seen := make(map[string]bool)
	for _, request := range fixture.queue.requests {
		if request.Type != TaskDocumentParsePoll {
			continue
		}
		if request.TaskID == "" {
			t.Fatal("poll task has empty TaskID")
		}
		if seen[request.TaskID] {
			t.Fatalf("duplicate poll TaskID %q", request.TaskID)
		}
		seen[request.TaskID] = true
	}
}

func assertQueuedLineage(t *testing.T, request queue.JobRequest, source DocumentTaskPayload, chunkSetID uuid.UUID) {
	t.Helper()
	var got DocumentTaskPayload
	if err := json.Unmarshal(request.Payload, &got); err != nil {
		t.Fatal(err)
	}
	if got.WorkspaceID != source.WorkspaceID || got.KnowledgeBaseID != source.KnowledgeBaseID ||
		got.DocumentID != source.DocumentID || got.DocumentRevisionID != source.DocumentRevisionID ||
		got.GenerationID != source.GenerationID || got.JobID == uuid.Nil || got.ChunkSetID != chunkSetID {
		t.Fatalf("queued payload = %#v, source = %#v", got, source)
	}
}

func processDocumentTask(ctx context.Context, typ string, handlers DocumentHandlers, payload DocumentTaskPayload) error {
	mux := asynq.NewServeMux()
	RegisterDocumentHandlers(mux, handlers)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	task := asynq.NewTask(typ, encoded)
	handler, pattern := mux.Handler(task)
	if pattern != typ {
		panic("handler not registered")
	}
	return handler.ProcessTask(ctx, task)
}

type documentTaskFixture struct {
	workspaceID, knowledgeBaseID, documentID, revisionID, generationID, jobID uuid.UUID
	store                                                                     *fakeDocumentTaskStore
	queue                                                                     *fakeDocumentTaskQueue
	pipeline                                                                  *fakeDocumentTaskPipeline
}

func newDocumentTaskFixture(kind value.DocumentKind, status value.DocumentRevisionStatus) *documentTaskFixture {
	fixture := &documentTaskFixture{
		workspaceID: uuid.New(), knowledgeBaseID: uuid.New(), documentID: uuid.New(),
		revisionID: uuid.New(), generationID: uuid.New(), jobID: uuid.New(),
		queue: &fakeDocumentTaskQueue{}, pipeline: &fakeDocumentTaskPipeline{},
	}
	fixture.store = &fakeDocumentTaskStore{
		job: &dto.Job{
			ID: fixture.jobID, WorkspaceID: fixture.workspaceID, KnowledgeBaseID: fixture.knowledgeBaseID,
			DocumentID: fixture.documentID, DocumentRevisionID: fixture.revisionID,
			Type: TaskDocumentParseStart, Status: value.JobStatusPending,
			Payload: map[string]any{"index_generation_id": fixture.generationID.String()},
		},
		revision: &model.DocumentRevision{
			ID: fixture.revisionID, WorkspaceID: fixture.workspaceID, KnowledgeBaseID: fixture.knowledgeBaseID,
			DocumentID: fixture.documentID, Kind: kind, Status: status,
		},
	}
	return fixture
}

func (f *documentTaskFixture) payload() DocumentTaskPayload {
	return DocumentTaskPayload{
		WorkspaceID: f.workspaceID, KnowledgeBaseID: f.knowledgeBaseID,
		DocumentID: f.documentID, DocumentRevisionID: f.revisionID,
		GenerationID: f.generationID, JobID: f.jobID,
	}
}

func (f *documentTaskFixture) handlers() DocumentHandlers {
	return DocumentHandlers{Store: f.store, Queue: f.queue, Pipeline: f.pipeline}
}

type fakeDocumentTaskStore struct {
	job         *dto.Job
	revision    *model.DocumentRevision
	published   bool
	withinCalls int
	failures    []taskFailure
	failErr     error
	marks       []value.JobStatus
	created     []*dto.Job
}

func (s *fakeDocumentTaskStore) WithinWorkspace(
	ctx context.Context,
	workspaceID uuid.UUID,
	fn func(context.Context, DocumentTaskTx) error,
) error {
	s.withinCalls++
	return fn(ctx, &fakeDocumentTaskTx{store: s, workspaceID: workspaceID})
}

func (s *fakeDocumentTaskStore) FailTask(
	_ context.Context,
	workspaceID, jobID, revisionID uuid.UUID,
	errorClass, message string,
) error {
	if s.failErr != nil {
		return s.failErr
	}
	s.failures = append(s.failures, taskFailure{
		workspaceID: workspaceID, jobID: jobID, revisionID: revisionID,
		errorClass: errorClass, message: message,
	})
	return nil
}

func (s *fakeDocumentTaskStore) MarkRunning(context.Context, uuid.UUID, uuid.UUID) error {
	s.marks = append(s.marks, value.JobStatusRunning)
	return nil
}

func (s *fakeDocumentTaskStore) MarkSucceeded(context.Context, uuid.UUID, uuid.UUID) error {
	s.marks = append(s.marks, value.JobStatusCompleted)
	return nil
}

func (s *fakeDocumentTaskStore) MarkFailed(context.Context, uuid.UUID, uuid.UUID, string) error {
	s.marks = append(s.marks, value.JobStatusFailed)
	return nil
}

func (s *fakeDocumentTaskStore) CreateNextForRevision(
	_ context.Context,
	workspaceID, knowledgeBaseID, documentID, revisionID, _ uuid.UUID,
	typ string,
	payload map[string]any,
) (*dto.Job, error) {
	job := &dto.Job{
		ID: uuid.New(), WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
		DocumentID: documentID, DocumentRevisionID: revisionID, Type: typ,
		Status: value.JobStatusPending, Payload: payload,
	}
	s.created = append(s.created, job)
	return job, nil
}

type fakeDocumentTaskTx struct {
	store       *fakeDocumentTaskStore
	workspaceID uuid.UUID
}

func (tx *fakeDocumentTaskTx) GetJob(context.Context, uuid.UUID) (*dto.Job, error) {
	return tx.store.job, nil
}

func (tx *fakeDocumentTaskTx) GetRevision(context.Context, uuid.UUID) (*model.DocumentRevision, error) {
	return tx.store.revision, nil
}

func (tx *fakeDocumentTaskTx) IsRevisionPublished(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return tx.store.published, nil
}

type taskFailure struct {
	workspaceID, jobID, revisionID uuid.UUID
	errorClass, message            string
}

type fakeDocumentTaskQueue struct {
	requests []queue.JobRequest
}

func (q *fakeDocumentTaskQueue) Enqueue(_ context.Context, request queue.JobRequest) (*queue.JobHandle, error) {
	q.requests = append(q.requests, request)
	return &queue.JobHandle{ID: uuid.NewString()}, nil
}

type pipelineCall struct {
	workspaceID, revisionID, generationID, chunkSetID uuid.UUID
}

type fakeDocumentTaskPipeline struct {
	chunkSetID uuid.UUID
	parseCalls []pipelineCall
	chunkCalls []pipelineCall
	indexCalls []pipelineCall
	parseErr   error
	chunkErr   error
	indexErr   error
}

func (p *fakeDocumentTaskPipeline) RunParse(_ context.Context, workspaceID, revisionID uuid.UUID) error {
	p.parseCalls = append(p.parseCalls, pipelineCall{workspaceID: workspaceID, revisionID: revisionID})
	return p.parseErr
}

func (p *fakeDocumentTaskPipeline) RunChunk(
	_ context.Context,
	workspaceID, revisionID, generationID uuid.UUID,
) (uuid.UUID, error) {
	p.chunkCalls = append(p.chunkCalls, pipelineCall{
		workspaceID: workspaceID, revisionID: revisionID, generationID: generationID,
	})
	if p.chunkSetID == uuid.Nil {
		p.chunkSetID = uuid.New()
	}
	return p.chunkSetID, p.chunkErr
}

func (p *fakeDocumentTaskPipeline) RunIndex(
	_ context.Context,
	workspaceID, generationID, chunkSetID uuid.UUID,
) ([]*model.RetrievalEntry, error) {
	p.indexCalls = append(p.indexCalls, pipelineCall{
		workspaceID: workspaceID, generationID: generationID, chunkSetID: chunkSetID,
	})
	return nil, p.indexErr
}

func assertJobMarks(t *testing.T, got []value.JobStatus, want ...value.JobStatus) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("job marks = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("job mark[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}
