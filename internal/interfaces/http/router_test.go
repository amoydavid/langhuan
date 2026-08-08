package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	stdhttp "net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// =====================================================================
// Fakes for the resource (non-auth) services.
// =====================================================================

type fakeKnowledgeBaseService struct {
	items       map[uuid.UUID]*dto.KnowledgeBase
	updateInput service.UpdateKnowledgeBaseBasicsInput
	createInput *service.CreateKnowledgeBaseInput
}

type fakeWorkspaceService struct {
	items     map[uuid.UUID]*dto.Workspace
	slugIndex map[string]*dto.Workspace
	// createForPlatform records the last CreateForPlatformAdmin invocation.
	createForPlatform     *service.CreateWorkspaceInput
	createForPlatformUser uuid.UUID
	createForPlatformErr  error
}

type fakeDocumentIngestService struct {
	input  service.IngestDocumentInput
	result *service.IngestDocumentResult
}

type fakeDocumentQueryService struct {
	items              map[uuid.UUID]*dto.Document
	deletedWorkspaceID uuid.UUID
	deletedDocumentID  uuid.UUID
}

type fakeJobQueryService struct {
	items map[uuid.UUID]*dto.Job
}

func newFakeKnowledgeBaseService() *fakeKnowledgeBaseService {
	return &fakeKnowledgeBaseService{items: make(map[uuid.UUID]*dto.KnowledgeBase)}
}

func newFakeWorkspaceService() *fakeWorkspaceService {
	return &fakeWorkspaceService{items: make(map[uuid.UUID]*dto.Workspace)}
}

func newFakeDocumentQueryService() *fakeDocumentQueryService {
	return &fakeDocumentQueryService{items: make(map[uuid.UUID]*dto.Document)}
}

func newFakeJobQueryService() *fakeJobQueryService {
	return &fakeJobQueryService{items: make(map[uuid.UUID]*dto.Job)}
}

func (s *fakeKnowledgeBaseService) Create(_ context.Context, input service.CreateKnowledgeBaseInput) (*dto.KnowledgeBase, error) {
	if input.Name == "" || input.EmbeddingModelID == uuid.Nil {
		return nil, domainerrors.ErrValidation
	}
	record := input
	s.createInput = &record
	now := time.Date(2026, 6, 17, 8, 0, 0, 0, time.UTC)
	kb := &dto.KnowledgeBase{
		ID: uuid.New(), WorkspaceID: input.WorkspaceID,
		Name: input.Name, Description: input.Description,
		EmbeddingModelID: input.EmbeddingModelID,
		EmbeddingModel:   dto.EmbeddingModelSummary{ID: input.EmbeddingModelID, Name: "embed", DisplayName: "Embedding", Provider: "openai", ProviderDisplayName: "OpenAI", Dimensions: 1024, Available: true},
		ChunkingConfig:   dto.ChunkingConfig{ChunkSize: 512, ChunkOverlap: 80}, Metadata: map[string]any{},
		SourceType: input.SourceType, SourceConfig: input.SourceConfig, SourceConnectionID: input.SourceConnectionID,
		CreatedAt: now, UpdatedAt: now,
	}
	if kb.Metadata == nil {
		kb.Metadata = map[string]any{}
	}
	s.items[kb.ID] = kb
	return kb, nil
}

func (s *fakeWorkspaceService) Create(_ context.Context, input service.CreateWorkspaceInput) (*dto.Workspace, error) {
	if input.Name == "" {
		return nil, domainerrors.ErrValidation
	}
	now := time.Date(2026, 6, 17, 8, 0, 0, 0, time.UTC)
	ws := &dto.Workspace{
		ID:        uuid.New(),
		Name:      input.Name,
		Slug:      input.Slug,
		Metadata:  map[string]any{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if ws.Metadata == nil {
		ws.Metadata = map[string]any{}
	}
	s.items[ws.ID] = ws
	if ws.Slug != "" {
		if s.slugIndex == nil {
			s.slugIndex = make(map[string]*dto.Workspace)
		}
		s.slugIndex[ws.Slug] = ws
	}
	return ws, nil
}

// CreateForPlatformAdmin records the create-by-admin input and returns a workspace.
func (s *fakeWorkspaceService) CreateForPlatformAdmin(_ context.Context, input service.CreateWorkspaceInput, creatorUserID uuid.UUID, creatorIsPlatformAdmin bool) (*dto.Workspace, error) {
	if s.createForPlatformErr != nil {
		return nil, s.createForPlatformErr
	}
	clone := input
	s.createForPlatform = &clone
	s.createForPlatformUser = creatorUserID
	_ = creatorIsPlatformAdmin
	return s.Create(context.Background(), input)
}

// GetBySlug resolves a workspace by slug for the middleware/handler.
func (s *fakeWorkspaceService) GetBySlug(_ context.Context, slug string) (*dto.Workspace, error) {
	if s.slugIndex == nil {
		return nil, domainerrors.ErrNotFound
	}
	ws, ok := s.slugIndex[slug]
	if !ok {
		return nil, domainerrors.ErrNotFound
	}
	return ws, nil
}

func (s *fakeKnowledgeBaseService) Get(_ context.Context, access value.ResourceAccess, id uuid.UUID) (*dto.KnowledgeBase, error) {
	kb, ok := s.items[id]
	if !ok || kb.WorkspaceID != access.WorkspaceID || (!access.Unrestricted && !access.AllowsKnowledgeBase(id)) {
		return nil, domainerrors.ErrNotFound
	}
	return kb, nil
}

func (s *fakeKnowledgeBaseService) List(_ context.Context, access value.ResourceAccess) ([]*dto.KnowledgeBase, error) {
	result := make([]*dto.KnowledgeBase, 0)
	for _, kb := range s.items {
		if kb.WorkspaceID == access.WorkspaceID && (access.Unrestricted || access.AllowsKnowledgeBase(kb.ID)) {
			result = append(result, kb)
		}
	}
	return result, nil
}

func (s *fakeKnowledgeBaseService) UpdateBasics(_ context.Context, input service.UpdateKnowledgeBaseBasicsInput) (*dto.KnowledgeBase, error) {
	s.updateInput = input
	kb, ok := s.items[input.KnowledgeBaseID]
	if !ok || kb.WorkspaceID != input.WorkspaceID {
		return nil, domainerrors.ErrNotFound
	}
	if input.Name != nil {
		kb.Name = *input.Name
	}
	if input.Description != nil {
		kb.Description = *input.Description
	}
	return kb, nil
}

func (s *fakeWorkspaceService) Get(_ context.Context, id uuid.UUID) (*dto.Workspace, error) {
	ws, ok := s.items[id]
	if !ok {
		return nil, domainerrors.ErrNotFound
	}
	return ws, nil
}

func (s *fakeDocumentIngestService) Ingest(_ context.Context, input service.IngestDocumentInput) (*service.IngestDocumentResult, error) {
	s.input = input
	if input.Reader != nil {
		_, _ = io.Copy(io.Discard, input.Reader)
	}
	if s.result != nil {
		return s.result, nil
	}
	now := time.Date(2026, 6, 17, 8, 0, 0, 0, time.UTC)
	doc := &dto.Document{
		ID:              uuid.New(),
		KnowledgeBaseID: input.KnowledgeBaseID,
		Title:           input.Title,
		FileType:        "md",
		SourceType:      "upload",
		Status:          value.DocumentStatusPending,
		SHA256:          "abc123",
		RawStorageKey:   "raw/doc.md",
		SizeBytes:       input.SizeBytes,
		ContentType:     input.ContentType,
		Metadata:        map[string]any{},
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	job := &dto.Job{
		ID:         uuid.New(),
		DocumentID: doc.ID,
		Type:       "document_parse_start",
		Status:     value.JobStatusQueued,
		Payload:    map[string]any{"document_id": doc.ID.String()},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	return &service.IngestDocumentResult{Document: doc, Job: job, Deduped: input.Dedupe}, nil
}

func (s *fakeDocumentQueryService) Get(_ context.Context, access value.ResourceAccess, id uuid.UUID) (*dto.Document, error) {
	doc, ok := s.items[id]
	if !ok || doc.Metadata["workspace_id"] != access.WorkspaceID.String() {
		return nil, domainerrors.ErrNotFound
	}
	if !access.Unrestricted && !access.AllowsKnowledgeBase(doc.KnowledgeBaseID) {
		return nil, domainerrors.ErrNotFound
	}
	return doc, nil
}

func (s *fakeDocumentQueryService) List(_ context.Context, filter service.DocumentListFilter) ([]*dto.Document, error) {
	result := make([]*dto.Document, 0)
	for _, doc := range s.items {
		if doc.KnowledgeBaseID == filter.KnowledgeBaseID && doc.Metadata["workspace_id"] == filter.WorkspaceID.String() && (filter.Kind == nil || doc.Kind == *filter.Kind) {
			result = append(result, doc)
		}
	}
	return result, nil
}

func (s *fakeDocumentQueryService) Delete(_ context.Context, access value.ResourceAccess, documentID uuid.UUID) error {
	doc, ok := s.items[documentID]
	if !ok || doc.Metadata["workspace_id"] != access.WorkspaceID.String() {
		return domainerrors.ErrNotFound
	}
	if !access.Unrestricted && !access.AllowsKnowledgeBase(doc.KnowledgeBaseID) {
		return domainerrors.ErrNotFound
	}
	s.deletedWorkspaceID = access.WorkspaceID
	s.deletedDocumentID = documentID
	delete(s.items, documentID)
	return nil
}

func (s *fakeJobQueryService) Get(_ context.Context, access value.ResourceAccess, id uuid.UUID) (*dto.Job, error) {
	job, ok := s.items[id]
	if !ok || job.Payload["workspace_id"] != access.WorkspaceID.String() {
		return nil, domainerrors.ErrNotFound
	}
	if !access.Unrestricted && !access.AllowsKnowledgeBase(job.KnowledgeBaseID) {
		return nil, domainerrors.ErrNotFound
	}
	return job, nil
}

// =====================================================================
// Basic / error-mapping tests (no auth involved).
// =====================================================================

func TestHealthz(t *testing.T) {
	router := NewRouter(Dependencies{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/healthz", nil)

	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" || body["service"] != "langhuan" {
		t.Fatalf("body = %#v", body)
	}
}

func TestAllRESTRoutesUseAPIV1(t *testing.T) {
	deps, _, _, _, _ := newAuthTestDeps()
	deps.Workspaces = newFakeWorkspaceService()
	deps.KnowledgeBases = newFakeKnowledgeBaseService()
	deps.DocumentIngest = &fakeDocumentIngestService{}
	deps.Documents = newFakeDocumentQueryService()
	deps.Jobs = newFakeJobQueryService()
	deps.MCPHandler = stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusNoContent)
	})

	for _, route := range NewRouter(deps).Routes() {
		if strings.HasPrefix(route.Path, "/mcp") {
			continue
		}
		if !strings.HasPrefix(route.Path, "/api/v1/") {
			t.Errorf("REST route escaped /api/v1: %s %s", route.Method, route.Path)
		}
	}
}

func TestUnknownAPIRouteReturnsJSON404(t *testing.T) {
	router := NewRouter(Dependencies{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(stdhttp.MethodGet, "/api/v1/unknown", nil))

	if rec.Code != stdhttp.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("content-type = %q, want application/json", rec.Header().Get("Content-Type"))
	}
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "not_found" {
		t.Fatalf("code = %q, want not_found", body.Error.Code)
	}
}

func TestLegacyRootRESTRoutesAreNotRegistered(t *testing.T) {
	deps, _, _, _, _ := newAuthTestDeps()
	deps.Workspaces = newFakeWorkspaceService()
	router := NewRouter(deps)

	tests := []struct {
		method string
		path   string
	}{
		{stdhttp.MethodGet, "/healthz"},
		{stdhttp.MethodPost, "/auth/login"},
		{stdhttp.MethodPost, "/auth/register"},
		{stdhttp.MethodPost, "/auth/logout"},
		{stdhttp.MethodGet, "/auth/me"},
		{stdhttp.MethodGet, "/invitations/token"},
		{stdhttp.MethodPost, "/admin/users/" + uuid.NewString() + "/password-reset"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != stdhttp.StatusNotFound {
				t.Fatalf("status = %d, want 404, body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHTTPStatusForError(t *testing.T) {
	statusCases := []struct {
		name   string
		err    error
		status int
	}{
		{"not_found", domainerrors.ErrNotFound, stdhttp.StatusNotFound},
		{"validation", domainerrors.ErrValidation, stdhttp.StatusBadRequest},
		{"unauthorized", domainerrors.ErrUnauthorized, stdhttp.StatusUnauthorized},
		{"forbidden", domainerrors.ErrForbidden, stdhttp.StatusForbidden},
		{"conflict", domainerrors.ErrConflict, stdhttp.StatusConflict},
		{"rate_limited", domainerrors.ErrRateLimited, stdhttp.StatusTooManyRequests},
		{"unsupported_file_type", domainerrors.ErrUnsupportedFileType, stdhttp.StatusUnsupportedMediaType},
		{"wrapped_not_found", fmt.Errorf("ctx: %w", domainerrors.ErrNotFound), stdhttp.StatusNotFound},
		{"default", errors.New("boom"), stdhttp.StatusInternalServerError},
	}
	for _, tc := range statusCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := statusForError(tc.err); got != tc.status {
				t.Fatalf("statusForError(%v) = %d, want %d", tc.err, got, tc.status)
			}
		})
	}

	codeCases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"not_found", domainerrors.ErrNotFound, stdhttp.StatusNotFound, "not_found"},
		{"validation", domainerrors.ErrValidation, stdhttp.StatusBadRequest, "validation_error"},
		{"unauthorized", domainerrors.ErrUnauthorized, stdhttp.StatusUnauthorized, "unauthorized"},
		{"forbidden", domainerrors.ErrForbidden, stdhttp.StatusForbidden, "forbidden"},
		{"conflict", domainerrors.ErrConflict, stdhttp.StatusConflict, "conflict"},
		{"rate_limited", domainerrors.ErrRateLimited, stdhttp.StatusTooManyRequests, "rate_limited"},
		{"unsupported_file_type", domainerrors.ErrUnsupportedFileType, stdhttp.StatusUnsupportedMediaType, "unsupported_file_type"},
		{"default", errors.New("boom"), stdhttp.StatusInternalServerError, "internal_error"},
	}
	for _, tc := range codeCases {
		t.Run("code/"+tc.name, func(t *testing.T) {
			router := gin.New()
			router.GET("/", func(c *gin.Context) {
				writeServiceError(c, tc.err)
			})
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
			router.ServeHTTP(rec, req)

			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tc.status, rec.Body.String())
			}
			var body errorBody
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Error.Code != tc.code {
				t.Fatalf("code = %q, want %q", body.Error.Code, tc.code)
			}
		})
	}
}

func TestUnsupportedFileTypeHTTPResponse(t *testing.T) {
	router := gin.New()
	router.GET("/", func(c *gin.Context) {
		writeServiceError(c, fmt.Errorf("文件扩展名 pdf: %w", domainerrors.ErrUnsupportedFileType))
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(stdhttp.MethodGet, "/", nil))

	if rec.Code != stdhttp.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", rec.Code, stdhttp.StatusUnsupportedMediaType)
	}
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "unsupported_file_type" {
		t.Fatalf("code = %q, want unsupported_file_type", body.Error.Code)
	}
	if body.Error.Message != "不支持的文件类型" {
		t.Fatalf("message = %q, want 不支持的文件类型", body.Error.Message)
	}
}

// TestWriteServiceErrorMasksInternalDetails 验证 500（非领域错误）响应体返回通用消息，
// 不泄漏原始错误细节（详情改为服务端日志记录）。
func TestWriteServiceErrorMasksInternalDetails(t *testing.T) {
	router := gin.New()
	secretDetail := "boom: pq: connection refused at 10.0.0.5:5432 (sensitive stack/dsn)"
	router.GET("/", func(c *gin.Context) {
		writeServiceError(c, errors.New(secretDetail))
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, stdhttp.StatusInternalServerError)
	}
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "internal_error" {
		t.Fatalf("code = %q, want internal_error", body.Error.Code)
	}
	// 响应消息必须是通用文案，不得包含原始错误细节。
	if strings.Contains(body.Error.Message, secretDetail) {
		t.Fatalf("500 响应泄漏内部错误细节: %q", body.Error.Message)
	}
	if strings.Contains(body.Error.Message, "pq:") || strings.Contains(body.Error.Message, "10.0.0.5") {
		t.Fatalf("500 响应疑似泄漏内部信息: %q", body.Error.Message)
	}
	if body.Error.Message == "" {
		t.Fatal("500 响应消息不应为空")
	}
}

func TestLegacyKnowledgeBaseRoutesAreNotRegistered(t *testing.T) {
	router := NewRouter(Dependencies{KnowledgeBases: newFakeKnowledgeBaseService()})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/knowledge-bases", bytes.NewBufferString(`{"name":"kb","embedding_model_id":"00000000-0000-0000-0000-000000000001"}`))
	req.Header.Set("content-type", "application/json")
	router.ServeHTTP(rec, req)
	if rec.Code != stdhttp.StatusNotFound {
		t.Fatalf("legacy create status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(stdhttp.MethodGet, "/api/v1/knowledge-bases/"+uuid.NewString(), nil)
	router.ServeHTTP(rec, req)
	if rec.Code != stdhttp.StatusNotFound {
		t.Fatalf("legacy get status = %d", rec.Code)
	}
}

// =====================================================================
// Slug fixtures (built on top of the auth fakes + resource fakes).
// =====================================================================

// slugResourceFixtures wires a router where the user is a member of the "acme"
// workspace with a given role, with resource services (KB/doc/job) configured
// by the caller via the returned fakes.
type slugResourceFixtures struct {
	router          *gin.Engine
	wsID            uuid.UUID
	userID          uuid.UUID
	sessionID       uuid.UUID
	auth            *fakeAuthService
	mbs             *fakeMembershipService
	wsSvc           *fakeWorkspaceService
	kbSvc           *fakeKnowledgeBaseService
	syncSvc         *fakeKnowledgeBaseSyncService
	sourcePolicySvc *fakeKnowledgeBaseSourcePolicyService
	docSvc          *fakeDocumentQueryService
	jobSvc          *fakeJobQueryService
	summarySvc      *fakeKnowledgeBaseSummaryHTTPService
	ingest          *fakeDocumentIngestService
	faq             *fakeFAQDocumentHTTPService
}

func newSlugResourceFixtures(t *testing.T, role value.WorkspaceRole, isPlatformAdmin bool) *slugResourceFixtures {
	t.Helper()
	deps, auth, users, mbs, invs := newAuthTestDeps()
	_ = users
	_ = invs

	wsID := uuid.New()
	userID := uuid.New()
	sessionID := uuid.New()

	auth.authUser = &model.User{ID: userID, IsPlatformAdmin: isPlatformAdmin}
	auth.sessionID = sessionID
	mbs.getResult = &dto.Membership{ID: uuid.New(), WorkspaceID: wsID, UserID: userID, Role: role}

	wsSvc := newFakeWorkspaceService()
	wsSvc.slugIndex = map[string]*dto.Workspace{
		"acme": {ID: wsID, Name: "Acme", Slug: "acme", Metadata: map[string]any{}},
	}
	wsSvc.items[wsID] = wsSvc.slugIndex["acme"]
	deps.Workspaces = wsSvc

	kbSvc := newFakeKnowledgeBaseService()
	deps.KnowledgeBases = kbSvc

	syncSvc := &fakeKnowledgeBaseSyncService{}
	deps.KnowledgeBaseSync = syncSvc

	sourcePolicySvc := &fakeKnowledgeBaseSourcePolicyService{}
	deps.KnowledgeBaseSourcePolicy = sourcePolicySvc

	docSvc := newFakeDocumentQueryService()
	deps.Documents = docSvc

	jobSvc := newFakeJobQueryService()
	deps.Jobs = jobSvc
	summarySvc := &fakeKnowledgeBaseSummaryHTTPService{}
	deps.KnowledgeBaseSummary = summarySvc

	ingest := &fakeDocumentIngestService{}
	deps.DocumentIngest = ingest
	faq := &fakeFAQDocumentHTTPService{result: &dto.FAQDocument{
		Questions: []string{"问题"},
		Answer:    "回答",
	}}
	deps.FAQDocuments = faq
	deps.FileTree = &fakeFileTreeHTTPService{}
	deps.DocumentChunks = &fakeDocumentChunksHTTPService{}
	deps.Models = &fakeModelHTTPService{}

	return &slugResourceFixtures{
		router:          NewRouter(deps),
		wsID:            wsID,
		userID:          userID,
		sessionID:       sessionID,
		auth:            auth,
		mbs:             mbs,
		wsSvc:           wsSvc,
		kbSvc:           kbSvc,
		syncSvc:         syncSvc,
		sourcePolicySvc: sourcePolicySvc,
		docSvc:          docSvc,
		jobSvc:          jobSvc,
		summarySvc:      summarySvc,
		ingest:          ingest,
		faq:             faq,
	}
}

// authedRequest issues an authenticated request to a slug route.
func (f *slugResourceFixtures) authedRequest(method, path string, body []byte, contentType string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.AddCookie(&stdhttp.Cookie{Name: testCookieName, Value: f.sessionID.String()})
	if contentType != "" {
		req.Header.Set("content-type", contentType)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func (f *slugResourceFixtures) plainRequest(method, path string, body []byte, contentType string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if contentType != "" {
		req.Header.Set("content-type", contentType)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

// =====================================================================
// Workspace slug + role-boundary tests.
// =====================================================================

// TestWorkspaceSlugRouteRequiresCookie: no cookie -> 401 on a workspace route.
func TestWorkspaceSlugRouteRequiresCookie(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleMember, false)

	rec := f.plainRequest(stdhttp.MethodGet, "/api/v1/workspaces/acme", nil, "")

	if rec.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (no cookie)", rec.Code)
	}
}

// TestWorkspaceGetBySlug: a member can GET their workspace via slug.
func TestWorkspaceGetBySlug(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleMember, false)

	rec := f.authedRequest(stdhttp.MethodGet, "/api/v1/workspaces/acme", nil, "")

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got dto.Workspace
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != f.wsID || got.Slug != "acme" {
		t.Fatalf("response = %#v", got)
	}
}

// TestWorkspaceCrossTenantReturns404: a member of acme GETs another tenant's slug -> 404.
func TestWorkspaceCrossTenantReturns404(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleMember, false)

	// "globex" is unknown to the resolver -> 404 (no leak).
	rec := f.authedRequest(stdhttp.MethodGet, "/api/v1/workspaces/globex", nil, "")
	if rec.Code != stdhttp.StatusNotFound {
		t.Fatalf("cross-tenant unknown slug status = %d, want 404", rec.Code)
	}

	// A known-but-not-a-member workspace also yields 404 (uniform not_found).
	otherID := uuid.New()
	f.wsSvc.slugIndex["globex"] = &dto.Workspace{ID: otherID, Name: "Globex", Slug: "globex", Metadata: map[string]any{}}
	f.wsSvc.items[otherID] = f.wsSvc.slugIndex["globex"]
	rec = f.authedRequest(stdhttp.MethodGet, "/api/v1/workspaces/globex", nil, "")
	if rec.Code != stdhttp.StatusNotFound {
		t.Fatalf("cross-tenant not-a-member status = %d, want 404", rec.Code)
	}
}

// TestWorkspaceLegacyUUIDURLReturns404: the old UUID path is no longer matched;
// a UUID passed where slug is expected resolves to no workspace -> 404.
func TestWorkspaceLegacyUUIDURLReturns404(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleMember, false)

	rec := f.authedRequest(stdhttp.MethodGet, "/api/v1/workspaces/"+f.wsID.String(), nil, "")
	if rec.Code != stdhttp.StatusNotFound {
		t.Fatalf("legacy UUID url status = %d, want 404", rec.Code)
	}
}

// TestPostWorkspaceByPlatformAdmin: platform admin creates a workspace (owner membership
// in same call via CreateForPlatformAdmin).
func TestPostWorkspaceByPlatformAdmin(t *testing.T) {
	deps, auth, _, _, _ := newAuthTestDeps()
	wsSvc := newFakeWorkspaceService()
	deps.Workspaces = wsSvc
	userID := uuid.New()
	sessionID := uuid.New()
	auth.authUser = &model.User{ID: userID, IsPlatformAdmin: true}
	auth.sessionID = sessionID
	router := NewRouter(deps)

	rec := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"name":"Acme","slug":"acme"}`)
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/workspaces", body)
	req.Header.Set("content-type", "application/json")
	req.AddCookie(&stdhttp.Cookie{Name: testCookieName, Value: sessionID.String()})
	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got dto.Workspace
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "Acme" || got.Slug != "acme" {
		t.Fatalf("response = %#v", got)
	}
	if wsSvc.createForPlatform == nil {
		t.Fatal("CreateForPlatformAdmin not called")
	}
	if wsSvc.createForPlatformUser != userID {
		t.Fatalf("creator user id = %s, want %s", wsSvc.createForPlatformUser, userID)
	}
}

func TestPostWorkspaceByPlatformAdminRejectsMetadata(t *testing.T) {
	deps, auth, _, _, _ := newAuthTestDeps()
	deps.Workspaces = newFakeWorkspaceService()
	sessionID := uuid.New()
	auth.authUser = &model.User{ID: uuid.New(), IsPlatformAdmin: true}
	auth.sessionID = sessionID
	router := NewRouter(deps)

	rec := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"name":"Acme","slug":"acme","metadata":{}}`)
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/workspaces", body)
	req.Header.Set("content-type", "application/json")
	req.AddCookie(&stdhttp.Cookie{Name: testCookieName, Value: sessionID.String()})
	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", rec.Code, rec.Body.String())
	}
}

// TestPostWorkspaceByNonAdminReturns403: a non-platform-admin cannot create workspaces.
func TestPostWorkspaceByNonAdminReturns403(t *testing.T) {
	deps, auth, _, _, _ := newAuthTestDeps()
	deps.Workspaces = newFakeWorkspaceService()
	sessionID := uuid.New()
	auth.authUser = &model.User{ID: uuid.New(), IsPlatformAdmin: false}
	auth.sessionID = sessionID
	router := NewRouter(deps)

	rec := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"name":"Acme","slug":"acme"}`)
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/workspaces", body)
	req.Header.Set("content-type", "application/json")
	req.AddCookie(&stdhttp.Cookie{Name: testCookieName, Value: sessionID.String()})
	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

// TestPostWorkspaceWithoutCookieReturns401.
func TestPostWorkspaceWithoutCookieReturns401(t *testing.T) {
	deps, _, _, _, _ := newAuthTestDeps()
	deps.Workspaces = newFakeWorkspaceService()
	router := NewRouter(deps)

	rec := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"name":"Acme","slug":"acme"}`)
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/workspaces", body)
	req.Header.Set("content-type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// =====================================================================
// Knowledge-base slug tests.
// =====================================================================

func TestPostWorkspaceKnowledgeBases(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleMember, false)
	modelID := uuid.New()

	body := []byte(fmt.Sprintf(`{"name":"kb","description":"desc","embedding_model_id":%q}`, modelID.String()))
	rec := f.authedRequest(stdhttp.MethodPost, "/api/v1/workspaces/acme/knowledge-bases", body, "application/json")

	if rec.Code != stdhttp.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got dto.KnowledgeBase
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.WorkspaceID != f.wsID {
		t.Fatalf("workspace_id = %s, want %s (must come from AuthContext)", got.WorkspaceID, f.wsID)
	}
}

func TestPostWorkspaceKnowledgeBasesRejectsMetadata(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleMember, false)
	body := []byte(fmt.Sprintf(`{"name":"kb","embedding_model_id":%q,"metadata":{}}`, uuid.NewString()))

	rec := f.authedRequest(stdhttp.MethodPost, "/api/v1/workspaces/acme/knowledge-bases", body, "application/json")

	if rec.Code != stdhttp.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", rec.Code, rec.Body.String())
	}
}

func TestPostWorkspaceKnowledgeBasesRejectsUnknownChunkingConfigField(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleMember, false)
	body := []byte(fmt.Sprintf(`{"name":"kb","embedding_model_id":%q,"chunking_config":{"chunk_size":512,"chunk_overlap":80,"metadata":1}}`, uuid.NewString()))

	rec := f.authedRequest(stdhttp.MethodPost, "/api/v1/workspaces/acme/knowledge-bases", body, "application/json")

	if rec.Code != stdhttp.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", rec.Code, rec.Body.String())
	}
}

func TestListWorkspaceKnowledgeBases(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleMember, false)
	f.kbSvc.items[uuid.New()] = &dto.KnowledgeBase{ID: uuid.New(), WorkspaceID: f.wsID, Name: "docs"}
	f.kbSvc.items[uuid.New()] = &dto.KnowledgeBase{ID: uuid.New(), WorkspaceID: uuid.New(), Name: "other"}

	rec := f.authedRequest(stdhttp.MethodGet, "/api/v1/workspaces/acme/knowledge-bases", nil, "")
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got []*dto.KnowledgeBase
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].WorkspaceID != f.wsID {
		t.Fatalf("response = %#v", got)
	}

	empty := newSlugResourceFixtures(t, value.RoleMember, false)
	emptyRec := empty.authedRequest(stdhttp.MethodGet, "/api/v1/workspaces/acme/knowledge-bases", nil, "")
	if strings.TrimSpace(emptyRec.Body.String()) != "[]" {
		t.Fatalf("empty response = %s, want []", emptyRec.Body.String())
	}

	if status := f.plainRequest(stdhttp.MethodGet, "/api/v1/workspaces/acme/knowledge-bases", nil, "").Code; status != stdhttp.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", status)
	}
	if status := f.authedRequest(stdhttp.MethodGet, "/api/v1/workspaces/globex/knowledge-bases", nil, "").Code; status != stdhttp.StatusNotFound {
		t.Fatalf("cross-workspace status = %d, want 404", status)
	}
}

func TestListKnowledgeBaseDocuments(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleMember, false)
	kbID := uuid.New()
	docID := uuid.New()
	f.docSvc.items[docID] = &dto.Document{
		ID:              docID,
		KnowledgeBaseID: kbID,
		Title:           "notes",
		Metadata:        map[string]any{"workspace_id": f.wsID.String()},
	}

	path := "/api/v1/workspaces/acme/knowledge-bases/" + kbID.String() + "/documents"
	rec := f.authedRequest(stdhttp.MethodGet, path, nil, "")
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got []*dto.Document
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != docID {
		t.Fatalf("response = %#v", got)
	}

	empty := newSlugResourceFixtures(t, value.RoleMember, false)
	emptyPath := "/api/v1/workspaces/acme/knowledge-bases/" + kbID.String() + "/documents"
	emptyRec := empty.authedRequest(stdhttp.MethodGet, emptyPath, nil, "")
	if strings.TrimSpace(emptyRec.Body.String()) != "[]" {
		t.Fatalf("empty response = %s, want []", emptyRec.Body.String())
	}
	if status := f.plainRequest(stdhttp.MethodGet, path, nil, "").Code; status != stdhttp.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", status)
	}
	if status := f.authedRequest(stdhttp.MethodGet, "/api/v1/workspaces/globex/knowledge-bases/"+kbID.String()+"/documents", nil, "").Code; status != stdhttp.StatusNotFound {
		t.Fatalf("cross-workspace status = %d, want 404", status)
	}
}

func TestDeleteWorkspaceDocument(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleMember, false)
	documentID := uuid.New()
	f.docSvc.items[documentID] = &dto.Document{
		ID: documentID, KnowledgeBaseID: uuid.New(), Title: "delete me",
		Metadata: map[string]any{"workspace_id": f.wsID.String()},
	}
	path := "/api/v1/workspaces/acme/documents/" + documentID.String()

	rec := f.authedRequest(stdhttp.MethodDelete, path, nil, "")
	if rec.Code != stdhttp.StatusNoContent {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if f.docSvc.deletedWorkspaceID != f.wsID || f.docSvc.deletedDocumentID != documentID {
		t.Fatalf("delete scope = workspace %s document %s", f.docSvc.deletedWorkspaceID, f.docSvc.deletedDocumentID)
	}
	if _, exists := f.docSvc.items[documentID]; exists {
		t.Fatal("document still exists in fake service")
	}
	if status := f.plainRequest(stdhttp.MethodDelete, path, nil, "").Code; status != stdhttp.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", status)
	}
	if status := f.authedRequest(stdhttp.MethodDelete, "/api/v1/workspaces/acme/documents/not-a-uuid", nil, "").Code; status != stdhttp.StatusBadRequest {
		t.Fatalf("invalid id status = %d, want 400", status)
	}
}

func TestBearerDocumentStatusAndDeleteRespectKnowledgeBaseBinding(t *testing.T) {
	deps, auth, _, mbs, _ := newAuthTestDeps()
	workspaceID := uuid.New()
	auth.authUser = &model.User{ID: uuid.New()}
	mbs.getResult = &dto.Membership{WorkspaceID: workspaceID, UserID: auth.authUser.ID, Role: value.RoleMember}
	wsSvc := newFakeWorkspaceService()
	wsSvc.slugIndex = map[string]*dto.Workspace{"acme": {ID: workspaceID, Slug: "acme", Metadata: map[string]any{}}}
	wsSvc.items[workspaceID] = wsSvc.slugIndex["acme"]
	docSvc := newFakeDocumentQueryService()
	allowedKB, deniedKB := uuid.New(), uuid.New()
	allowedDoc, deniedDoc := uuid.New(), uuid.New()
	docSvc.items[allowedDoc] = &dto.Document{ID: allowedDoc, WorkspaceID: workspaceID, KnowledgeBaseID: allowedKB, Metadata: map[string]any{"workspace_id": workspaceID.String()}}
	docSvc.items[deniedDoc] = &dto.Document{ID: deniedDoc, WorkspaceID: workspaceID, KnowledgeBaseID: deniedKB, Metadata: map[string]any{"workspace_id": workspaceID.String()}}
	deps.Workspaces = wsSvc
	deps.Documents = docSvc
	deps.PublicURLs = mustPublicURLs(t)
	deps.APIKeyAuth = &fakeAPIKeyAuthenticator{principal: value.NewAPIKeyAuthContext(uuid.New(), workspaceID, []value.APIScope{value.ScopeDocumentsRead, value.ScopeDocumentsWrite}, []uuid.UUID{allowedKB})}
	router := NewRouter(deps)

	request := func(method, documentID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1/workspaces/acme/documents/"+documentID, nil)
		req.Header.Set("Authorization", "Bearer lhk_test")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	if rec := request(stdhttp.MethodGet, allowedDoc.String()); rec.Code != stdhttp.StatusOK {
		t.Fatalf("allowed status query = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec := request(stdhttp.MethodGet, deniedDoc.String()); rec.Code != stdhttp.StatusNotFound {
		t.Fatalf("out-of-scope status query = %d, want 404", rec.Code)
	}
	if rec := request(stdhttp.MethodDelete, deniedDoc.String()); rec.Code != stdhttp.StatusNotFound {
		t.Fatalf("out-of-scope delete = %d, want 404", rec.Code)
	}
	if _, ok := docSvc.items[deniedDoc]; !ok {
		t.Fatal("out-of-scope delete removed the document")
	}
	if rec := request(stdhttp.MethodDelete, allowedDoc.String()); rec.Code != stdhttp.StatusNoContent {
		t.Fatalf("allowed delete = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestCreateKnowledgeBaseValidatesInput(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleMember, false)

	for _, payload := range []string{
		fmt.Sprintf(`{"embedding_model_id":%q}`, uuid.NewString()),
		`{"name":"kb","embedding_model_id":"not-a-uuid"}`,
	} {
		rec := f.authedRequest(stdhttp.MethodPost, "/api/v1/workspaces/acme/knowledge-bases", []byte(payload), "application/json")
		if rec.Code != stdhttp.StatusBadRequest {
			t.Fatalf("payload %s status = %d", payload, rec.Code)
		}
	}
}

func TestGetWorkspaceKnowledgeBase(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleMember, false)
	created, err := f.kbSvc.Create(context.Background(), service.CreateKnowledgeBaseInput{
		WorkspaceID: f.wsID, Name: "kb", EmbeddingModelID: uuid.New(),
	})
	if err != nil {
		t.Fatal(err)
	}

	rec := f.authedRequest(stdhttp.MethodGet, "/api/v1/workspaces/acme/knowledge-bases/"+created.ID.String(), nil, "")
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got dto.KnowledgeBase
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID || got.WorkspaceID != f.wsID {
		t.Fatalf("response = %#v", got)
	}
}

func TestKnowledgeBaseSummaryRoutesAreRegisteredForMembers(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleMember, false)
	knowledgeBaseID := uuid.New()
	f.summarySvc.summary = &dto.KnowledgeBaseSummary{
		KnowledgeBaseID: knowledgeBaseID, KnowledgeBaseName: "产品文档",
		SyncState: dto.KnowledgeBaseSyncSynced,
	}
	f.summarySvc.jobs = &dto.JobSummaryPage{Items: []*dto.JobSummary{}}

	summaryPath := "/api/v1/workspaces/acme/knowledge-bases/" + knowledgeBaseID.String() + "/summary"
	if recorder := f.authedRequest(stdhttp.MethodGet, summaryPath, nil, ""); recorder.Code != stdhttp.StatusOK {
		t.Fatalf("summary status/body = %d %s", recorder.Code, recorder.Body.String())
	}
	jobsPath := "/api/v1/workspaces/acme/knowledge-bases/" + knowledgeBaseID.String() + "/jobs?limit=20"
	if recorder := f.authedRequest(stdhttp.MethodGet, jobsPath, nil, ""); recorder.Code != stdhttp.StatusOK {
		t.Fatalf("jobs status/body = %d %s", recorder.Code, recorder.Body.String())
	}
	if f.summarySvc.workspaceID != f.wsID || f.summarySvc.knowledgeBaseID != knowledgeBaseID {
		t.Fatalf("summary lineage = %s/%s", f.summarySvc.workspaceID, f.summarySvc.knowledgeBaseID)
	}
}

// =====================================================================
// Document + job slug tests.
// =====================================================================

func TestPostWorkspaceKnowledgeBaseDocuments(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleMember, false)
	kbID := uuid.New()

	body, contentType := multipartDocumentBody(t, map[string]string{
		"title":       "Launch notes",
		"source_type": "upload",
	}, "file", "notes.md", "text/markdown", "hello")

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/workspaces/acme/knowledge-bases/"+kbID.String()+"/documents?dedupe=true", body)
	req.Header.Set("content-type", contentType)
	req.AddCookie(&stdhttp.Cookie{Name: testCookieName, Value: f.sessionID.String()})
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if f.ingest.input.WorkspaceID != f.wsID {
		t.Fatalf("workspace_id = %s, want %s (must come from AuthContext)", f.ingest.input.WorkspaceID, f.wsID)
	}
	if f.ingest.input.KnowledgeBaseID != kbID {
		t.Fatalf("knowledge_base_id = %s, want %s", f.ingest.input.KnowledgeBaseID, kbID)
	}
	if !f.ingest.input.Dedupe {
		t.Fatal("dedupe should be true")
	}
}

func TestPostWorkspaceKnowledgeBaseDocumentsRejectsMetadata(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleMember, false)
	kbID := uuid.New()
	body, contentType := multipartDocumentBody(t, map[string]string{
		"title":    "Launch notes",
		"metadata": `{}`,
	}, "file", "notes.md", "text/markdown", "hello")

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/workspaces/acme/knowledge-bases/"+kbID.String()+"/documents", body)
	req.Header.Set("content-type", contentType)
	req.AddCookie(&stdhttp.Cookie{Name: testCookieName, Value: f.sessionID.String()})
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusBadRequest {
		t.Fatalf("status = %d, body = %s, want 400", rec.Code, rec.Body.String())
	}
}

func TestGetWorkspaceDocument(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleMember, false)
	docID := uuid.New()
	f.docSvc.items[docID] = &dto.Document{
		ID:              docID,
		KnowledgeBaseID: uuid.New(),
		Title:           "Launch notes",
		Status:          value.DocumentStatusCompleted,
		Metadata:        map[string]any{"workspace_id": f.wsID.String()},
	}

	rec := f.authedRequest(stdhttp.MethodGet, "/api/v1/workspaces/acme/documents/"+docID.String(), nil, "")
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got dto.Document
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != docID {
		t.Fatalf("response = %#v", got)
	}
}

func TestMemberCanCreateReadAndUpdateFAQDocument(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleMember, false)
	knowledgeBaseID, documentID, baseRevisionID := uuid.New(), uuid.New(), uuid.New()

	createBody := []byte(`{"title":"退款","questions":["如何退款？"],"answer":"请在订单页申请。"}`)
	createPath := "/api/v1/workspaces/acme/knowledge-bases/" + knowledgeBaseID.String() + "/documents/faq"
	if recorder := f.authedRequest(stdhttp.MethodPost, createPath, createBody, "application/json"); recorder.Code != stdhttp.StatusCreated {
		t.Fatalf("create status/body = %d %s", recorder.Code, recorder.Body.String())
	}
	if f.faq.createInput == nil || f.faq.createInput.WorkspaceID != f.wsID ||
		f.faq.createInput.KnowledgeBaseID != knowledgeBaseID {
		t.Fatalf("create input = %#v", f.faq.createInput)
	}

	documentPath := "/api/v1/workspaces/acme/knowledge-bases/" + knowledgeBaseID.String() + "/documents/" + documentID.String() + "/faq"
	if recorder := f.authedRequest(stdhttp.MethodGet, documentPath, nil, ""); recorder.Code != stdhttp.StatusOK {
		t.Fatalf("get status/body = %d %s", recorder.Code, recorder.Body.String())
	}
	if f.faq.getWorkspaceID != f.wsID || f.faq.getDocumentID != documentID {
		t.Fatalf("get lineage = workspace %s document %s", f.faq.getWorkspaceID, f.faq.getDocumentID)
	}

	updateBody := []byte(`{"base_revision_id":"` + baseRevisionID.String() + `","questions":["退款流程？"],"answer":"打开订单页。"}`)
	if recorder := f.authedRequest(stdhttp.MethodPut, documentPath, updateBody, "application/json"); recorder.Code != stdhttp.StatusAccepted {
		t.Fatalf("update status/body = %d %s", recorder.Code, recorder.Body.String())
	}
	if f.faq.updateInput == nil || f.faq.updateInput.WorkspaceID != f.wsID ||
		f.faq.updateInput.DocumentID != documentID || f.faq.updateInput.BaseRevisionID != baseRevisionID {
		t.Fatalf("update input = %#v", f.faq.updateInput)
	}
}

func TestGetWorkspaceJob(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleMember, false)
	jobID := uuid.New()
	documentID := uuid.New()
	f.jobSvc.items[jobID] = &dto.Job{
		ID:         jobID,
		DocumentID: documentID,
		Type:       "document_parse_start",
		Status:     value.JobStatusQueued,
		Payload:    map[string]any{"workspace_id": f.wsID.String()},
	}

	rec := f.authedRequest(stdhttp.MethodGet, "/api/v1/workspaces/acme/jobs/"+jobID.String(), nil, "")
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got dto.Job
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != jobID {
		t.Fatalf("response = %#v", got)
	}
}

func multipartDocumentBody(t *testing.T, fields map[string]string, fieldName, fileName, contentType, content string) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := writer.CreatePart(textprotoMIMEHeader(map[string]string{
		"Content-Disposition": `form-data; name="` + fieldName + `"; filename="` + fileName + `"`,
		"Content-Type":        contentType,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body, writer.FormDataContentType()
}

func textprotoMIMEHeader(values map[string]string) textproto.MIMEHeader {
	header := make(textproto.MIMEHeader, len(values))
	for key, value := range values {
		header.Set(key, value)
	}
	return header
}
