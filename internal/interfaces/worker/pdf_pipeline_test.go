package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/pipeline"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
	parserport "github.com/dajee/langhuan/internal/ports/parser"
	portstorage "github.com/dajee/langhuan/internal/ports/storage"
)

// fakeAsyncParser 实现 DocumentParser + AsyncDocumentParser，
// 模拟 MinerU 的 Start/Poll 流程。
type fakeAsyncParser struct {
	startResult *parserport.AsyncParseStart
	pollResult  *parserport.AsyncParsePollResult
	startCalls  int
	pollCalls   int
	lastStartIn parserport.AsyncParseInput
	lastPollIn  parserport.AsyncParsePollInput
}

func (p *fakeAsyncParser) Supports(fileType string) bool {
	return fileType == "pdf"
}

func (p *fakeAsyncParser) Parse(_ context.Context, _ parserport.ParseInput) (*parserport.ParsedDocument, error) {
	return nil, fmt.Errorf("%w: async only", parserport.ErrUnsupportedFileType)
}

func (p *fakeAsyncParser) Start(_ context.Context, input parserport.AsyncParseInput) (*parserport.AsyncParseStart, error) {
	p.startCalls++
	p.lastStartIn = input
	if p.startResult != nil {
		return p.startResult, nil
	}
	return &parserport.AsyncParseStart{
		ExternalJobID: "fake-mineru-batch-001",
		Status:        parserport.AsyncSubmitted,
		Payload:       map[string]any{"batch_id": "fake-mineru-batch-001", "model_version": "vlm"},
	}, nil
}

func (p *fakeAsyncParser) Poll(_ context.Context, input parserport.AsyncParsePollInput) (*parserport.AsyncParsePollResult, error) {
	p.pollCalls++
	p.lastPollIn = input
	if p.pollResult != nil {
		return p.pollResult, nil
	}
	return &parserport.AsyncParsePollResult{
		Status: parserport.AsyncSucceeded,
		Document: &parserport.ParsedDocument{
			Markdown: "# 测试标题\n\n这是 PDF 解析产出的正文。",
			Manifest: model.ParseManifest{
				Version:       model.CurrentParseManifestVersion,
				Parser:        "pdf",
				ParserVersion: 1,
				Blocks: []model.ParsedBlock{
					{Sequence: 0, Kind: model.BlockKindParagraph, NormalizedStart: 0, NormalizedEnd: 30},
				},
			},
		},
	}, nil
}

// fakeParserRegistry 实现 worker.ParserRegistry。
type fakeParserRegistry struct {
	parser parserport.DocumentParser
}

func (r *fakeParserRegistry) Get(fileType string) (parserport.DocumentParser, error) {
	if r.parser != nil && r.parser.Supports(fileType) {
		return r.parser, nil
	}
	return nil, fmt.Errorf("%w: %s", parserport.ErrUnsupportedFileType, fileType)
}

// fakeAsyncPipeline 实现 DocumentPipeline + AsyncParseSupport。
type fakeAsyncPipeline struct {
	fakeDocumentTaskPipeline
	asyncParseCalls int
	lastParsed      *parserport.ParsedDocument
}

func (p *fakeAsyncPipeline) CompleteAsyncParse(
	_ context.Context,
	workspaceID, revisionID uuid.UUID,
	parsed *parserport.ParsedDocument,
	_ *pipeline.AssetResolver,
) error {
	p.asyncParseCalls++
	p.lastParsed = parsed
	return nil
}

// fakeAssetStoreForWorker 是简单 AssetStore，用于 AssetStoreFactory。
type fakeAssetStoreForWorker struct{}

func (s *fakeAssetStoreForWorker) Put(_ context.Context, obj portstorage.ObjectInput) (*portstorage.StoredObject, error) {
	return &portstorage.StoredObject{Key: obj.Key, PublicURL: "https://cdn/" + obj.Key, SizeBytes: int64(len(obj.Data))}, nil
}
func (s *fakeAssetStoreForWorker) Delete(_ context.Context, _ string) error { return nil }
func (s *fakeAssetStoreForWorker) Open(_ context.Context, _ string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

func TestPDFPipelineParseStartEnqueuesPollWithExternalJobID(t *testing.T) {
	asyncParser := &fakeAsyncParser{}
	fixture := newPDFPipelineFixture(asyncParser, parserport.AsyncSucceeded)
	payload := fixture.payload()

	// Set revision to pending file with pdf file_type
	fixture.store.revision.FileType = "pdf"
	fixture.store.revision.RawStorageKey = "raw/test.pdf"

	err := processDocumentTask(context.Background(), TaskDocumentParseStart, fixture.handlers(), payload)
	if err != nil {
		t.Fatalf("parse_start error = %v", err)
	}

	// Start should have been called
	if asyncParser.startCalls != 1 {
		t.Fatalf("asyncParser.startCalls = %d, want 1", asyncParser.startCalls)
	}

	// A parse_poll task should have been enqueued
	if len(fixture.queue.requests) != 1 {
		t.Fatalf("queue requests = %d, want 1", len(fixture.queue.requests))
	}
	if fixture.queue.requests[0].Type != TaskDocumentParsePoll {
		t.Fatalf("enqueued type = %q, want %q", fixture.queue.requests[0].Type, TaskDocumentParsePoll)
	}

	// The created job should carry external_job_id in payload
	if len(fixture.store.created) != 1 {
		t.Fatalf("created jobs = %d, want 1", len(fixture.store.created))
	}
	createdJob := fixture.store.created[0]
	extID, ok := createdJob.Payload["external_job_id"].(string)
	if !ok || extID == "" {
		t.Fatalf("created job payload missing external_job_id: %#v", createdJob.Payload)
	}
	if extID != "fake-mineru-batch-001" {
		t.Fatalf("external_job_id = %q, want fake-mineru-batch-001", extID)
	}
}

func TestPDFPipelineParsePollSucceededRunsCompleteAsyncParseAndEnqueuesIndex(t *testing.T) {
	asyncParser := &fakeAsyncParser{}
	fixture := newPDFPipelineFixture(asyncParser, parserport.AsyncSucceeded)
	payload := fixture.payload()

	// Simulate poll phase: revision still pending, job has external_job_id
	fixture.store.revision.FileType = "pdf"
	fixture.store.revision.Status = value.DocumentRevisionPending
	fixture.store.job.Type = TaskDocumentParsePoll
	fixture.store.job.Payload["external_job_id"] = "fake-mineru-batch-001"

	err := processDocumentTask(context.Background(), TaskDocumentParsePoll, fixture.handlers(), payload)
	if err != nil {
		t.Fatalf("parse_poll error = %v", err)
	}

	// Poll should have been called once
	if asyncParser.pollCalls != 1 {
		t.Fatalf("asyncParser.pollCalls = %d, want 1", asyncParser.pollCalls)
	}

	// CompleteAsyncParse should have been called with the parsed document
	if fixture.asyncPipeline.asyncParseCalls != 1 {
		t.Fatalf("CompleteAsyncParse calls = %d, want 1", fixture.asyncPipeline.asyncParseCalls)
	}
	if fixture.asyncPipeline.lastParsed == nil {
		t.Fatal("lastParsed is nil")
	}
	if !strings.Contains(fixture.asyncPipeline.lastParsed.Markdown, "测试标题") {
		t.Fatalf("parsed markdown = %q", fixture.asyncPipeline.lastParsed.Markdown)
	}

	// document_index should have been enqueued
	indexFound := false
	for _, req := range fixture.queue.requests {
		if req.Type == TaskDocumentIndex {
			indexFound = true
		}
	}
	if !indexFound {
		t.Fatal("document_index was not enqueued after successful poll")
	}
}

func TestPDFPipelineParsePollRunningRequeues(t *testing.T) {
	asyncParser := &fakeAsyncParser{
		pollResult: &parserport.AsyncParsePollResult{
			Status:     parserport.AsyncRunning,
			RetryAfter: 0, // fake queue ignores delay
			Payload:    map[string]any{"poll_count": 1},
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

	// CompleteAsyncParse should NOT have been called (still running)
	if fixture.asyncPipeline.asyncParseCalls != 0 {
		t.Fatalf("CompleteAsyncParse calls = %d, want 0 (running)", fixture.asyncPipeline.asyncParseCalls)
	}

	// parse_poll should have been re-enqueued
	pollFound := false
	for _, req := range fixture.queue.requests {
		if req.Type == TaskDocumentParsePoll {
			pollFound = true
		}
	}
	if !pollFound {
		t.Fatal("parse_poll was not re-enqueued for running state")
	}
}

func TestPDFPipelineParsePollFailedMarksJobFailed(t *testing.T) {
	asyncParser := &fakeAsyncParser{
		pollResult: &parserport.AsyncParsePollResult{
			Status:       parserport.AsyncFailed,
			ErrorCode:    "parse_error",
			ErrorMessage: "无法解析 PDF",
		},
	}
	fixture := newPDFPipelineFixture(asyncParser, parserport.AsyncFailed)
	payload := fixture.payload()

	fixture.store.revision.FileType = "pdf"
	fixture.store.revision.Status = value.DocumentRevisionPending
	fixture.store.job.Type = TaskDocumentParsePoll
	fixture.store.job.Payload["external_job_id"] = "fake-mineru-batch-001"

	err := processDocumentTask(context.Background(), TaskDocumentParsePoll, fixture.handlers(), payload)
	if err == nil {
		t.Fatal("expected error for failed parse")
	}

	// The job should have been marked as failed
	if len(fixture.store.failures) == 0 {
		t.Fatal("expected task failure to be recorded")
	}
	failure := fixture.store.failures[0]
	if !strings.Contains(failure.message, "parse_error") && !strings.Contains(failure.message, "无法解析") {
		t.Fatalf("failure message = %q", failure.message)
	}
}

// newPDFPipelineFixture 创建用于 PDF 异步解析测试的 fixture。
type pdfPipelineFixture struct {
	documentTaskFixture
	asyncPipeline *fakeAsyncPipeline
	asyncParser   *fakeAsyncParser
}

func newPDFPipelineFixture(asyncParser *fakeAsyncParser, _ parserport.AsyncStatus) *pdfPipelineFixture {
	f := &pdfPipelineFixture{
		documentTaskFixture: documentTaskFixture{
			workspaceID: uuid.New(), knowledgeBaseID: uuid.New(), documentID: uuid.New(),
			revisionID: uuid.New(), generationID: uuid.New(), jobID: uuid.New(),
			queue: &fakeDocumentTaskQueue{},
		},
		asyncPipeline: &fakeAsyncPipeline{},
		asyncParser:   asyncParser,
	}
	f.store = &fakeDocumentTaskStore{
		job: &dto.Job{
			ID: f.jobID, WorkspaceID: f.workspaceID, KnowledgeBaseID: f.knowledgeBaseID,
			DocumentID: f.documentID, DocumentRevisionID: f.revisionID,
			Type: TaskDocumentParseStart, Status: value.JobStatusPending,
			Payload: map[string]any{"index_generation_id": f.generationID.String()},
		},
		revision: &model.DocumentRevision{
			ID: f.revisionID, WorkspaceID: f.workspaceID, KnowledgeBaseID: f.knowledgeBaseID,
			DocumentID: f.documentID, Kind: value.DocumentKindFile, Status: value.DocumentRevisionPending,
		},
	}
	f.asyncPipeline.fakeDocumentTaskPipeline = fakeDocumentTaskPipeline{}
	return f
}

func (f *pdfPipelineFixture) handlers() DocumentHandlers {
	return DocumentHandlers{
		Store:          f.store,
		Queue:          f.queue,
		Pipeline:       f.asyncPipeline,
		ParserRegistry: &fakeParserRegistry{parser: f.asyncParser},
		AssetStoreFactory: func(workspaceID, knowledgeBaseID, documentID, revisionID uuid.UUID) *pipeline.AssetResolver {
			return pipeline.NewAssetResolver(
				&fakeAssetStoreForWorker{},
				nil, // no HTTP client needed in this unit test
				AssetsCfgForTest(),
				workspaceID, knowledgeBaseID, documentID, revisionID,
			)
		},
	}
}

func AssetsCfgForTest() config.AssetsStorageConfig {
	return config.AssetsStorageConfig{
		MaxCountPerDocument: 500,
		MaxImageSizeBytes:   10 * 1024 * 1024,
		AllowedMimeTypes:    []string{"image/png", "image/jpeg"},
	}
}
