package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"reflect"
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

type fakeModelProviderHTTPService struct {
	createWorkspaceInput service.CreateModelProviderInput
	createWorkspaceID    uuid.UUID
	createPlatformInput  service.CreateModelProviderInput
	createCalls          int
	updateWorkspaceInput service.UpdateModelProviderInput
	updateWorkspaceID    uuid.UUID
	updateProviderID     uuid.UUID
	updatePlatformInput  service.UpdateModelProviderInput
	deleteWorkspaceID    uuid.UUID
	deleteProviderID     uuid.UUID
	catalogWorkspaceID   uuid.UUID
	catalogProviderID    uuid.UUID
	catalogFilter        service.ModelCatalogFilter
	result               *dto.ModelProvider
	err                  error
}

func (s *fakeModelProviderHTTPService) provider() *dto.ModelProvider {
	if s.result != nil {
		return s.result
	}
	return &dto.ModelProvider{
		ID: uuid.New(), Scope: value.ModelScopeWorkspace, Name: "openai-prod",
		DisplayName: "OpenAI Production", Provider: "openai",
		Config:                map[string]any{"timeout_seconds": float64(60)},
		CredentialsConfigured: true, CredentialFields: []string{"api_key"},
		Status: value.ModelStatusActive, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
}

func (s *fakeModelProviderHTTPService) CreateWorkspace(_ context.Context, workspaceID uuid.UUID, input service.CreateModelProviderInput) (*dto.ModelProvider, error) {
	s.createCalls++
	s.createWorkspaceID, s.createWorkspaceInput = workspaceID, input
	return s.provider(), s.err
}

func (s *fakeModelProviderHTTPService) CreatePlatform(_ context.Context, input service.CreateModelProviderInput) (*dto.ModelProvider, error) {
	s.createCalls++
	s.createPlatformInput = input
	result := s.provider()
	result.Scope, result.WorkspaceID = value.ModelScopePlatform, nil
	return result, s.err
}

func (s *fakeModelProviderHTTPService) ListWorkspace(context.Context, uuid.UUID) ([]*dto.ModelProvider, error) {
	return []*dto.ModelProvider{s.provider()}, s.err
}

func (s *fakeModelProviderHTTPService) ListPlatform(context.Context) ([]*dto.ModelProvider, error) {
	result := s.provider()
	result.Scope, result.WorkspaceID = value.ModelScopePlatform, nil
	return []*dto.ModelProvider{result}, s.err
}

func (s *fakeModelProviderHTTPService) GetWorkspace(context.Context, uuid.UUID, uuid.UUID) (*dto.ModelProvider, error) {
	return s.provider(), s.err
}

func (s *fakeModelProviderHTTPService) GetPlatform(context.Context, uuid.UUID) (*dto.ModelProvider, error) {
	result := s.provider()
	result.Scope, result.WorkspaceID = value.ModelScopePlatform, nil
	return result, s.err
}

func (s *fakeModelProviderHTTPService) UpdateWorkspace(_ context.Context, workspaceID, providerID uuid.UUID, input service.UpdateModelProviderInput) (*dto.ModelProvider, error) {
	s.updateWorkspaceID, s.updateProviderID, s.updateWorkspaceInput = workspaceID, providerID, input
	return s.provider(), s.err
}

func (s *fakeModelProviderHTTPService) UpdatePlatform(_ context.Context, providerID uuid.UUID, input service.UpdateModelProviderInput) (*dto.ModelProvider, error) {
	s.updateProviderID, s.updatePlatformInput = providerID, input
	result := s.provider()
	result.Scope, result.WorkspaceID = value.ModelScopePlatform, nil
	return result, s.err
}

func (s *fakeModelProviderHTTPService) DeleteWorkspace(_ context.Context, workspaceID, providerID uuid.UUID) error {
	s.deleteWorkspaceID, s.deleteProviderID = workspaceID, providerID
	return s.err
}

func (s *fakeModelProviderHTTPService) DeletePlatform(_ context.Context, providerID uuid.UUID) error {
	s.deleteProviderID = providerID
	return s.err
}

func (s *fakeModelProviderHTTPService) SupportedProviders() []string {
	return []string{"openai", "ark", "ollama", "dashscope", "tencentcloud", "mineru"}
}

func (s *fakeModelProviderHTTPService) ProviderOptions() []service.ProviderOption {
	return []service.ProviderOption{
		{Key: "openai", Capabilities: []value.ProviderCapability{value.CapabilityEmbedding}},
		{Key: "rerank_compatible", Capabilities: []value.ProviderCapability{value.CapabilityRerank}},
	}
}

func (s *fakeModelProviderHTTPService) ListModelCatalogWorkspace(_ context.Context, workspaceID, providerID uuid.UUID, filter service.ModelCatalogFilter) (*dto.ModelCatalogResponse, error) {
	s.catalogWorkspaceID, s.catalogProviderID, s.catalogFilter = workspaceID, providerID, filter
	return &dto.ModelCatalogResponse{Provider: "openai", Items: []dto.ModelCatalogItem{}}, s.err
}

func (s *fakeModelProviderHTTPService) ListModelCatalogPlatform(_ context.Context, providerID uuid.UUID, filter service.ModelCatalogFilter) (*dto.ModelCatalogResponse, error) {
	s.catalogProviderID, s.catalogFilter = providerID, filter
	return &dto.ModelCatalogResponse{Provider: "openai", Items: []dto.ModelCatalogItem{}}, s.err
}

type fakeModelHTTPService struct {
	createWorkspaceID uuid.UUID
	createInput       service.CreateModelInput
	updateWorkspaceID uuid.UUID
	updateModelID     uuid.UUID
	updateInput       service.UpdateModelInput
	deleteWorkspaceID uuid.UUID
	deleteModelID     uuid.UUID
	listSelectType    value.ModelType
	listSelectActive  bool
	listManaged       bool
	listFilter        service.ModelListFilter
	result            *dto.Model
	err               error
}

func (s *fakeModelHTTPService) item() *dto.Model {
	if s.result != nil {
		return s.result
	}
	return &dto.Model{
		ID: uuid.New(), ProviderID: uuid.New(), Name: "text-embedding-3-large",
		DisplayName: "Text Embedding 3 Large", Type: value.ModelTypeEmbedding,
		ModelName: "text-embedding-3-large", Dimensions: modelRoutesIntPtr(1024),
		Parameters: map[string]any{"batch_size": float64(32)}, Status: value.ModelStatusActive,
		Available: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
}

func (s *fakeModelHTTPService) CreateWorkspace(_ context.Context, workspaceID uuid.UUID, input service.CreateModelInput) (*dto.Model, error) {
	s.createWorkspaceID, s.createInput = workspaceID, input
	return s.item(), s.err
}

func (s *fakeModelHTTPService) CreatePlatform(_ context.Context, input service.CreateModelInput) (*dto.Model, error) {
	s.createInput = input
	return s.item(), s.err
}

func (s *fakeModelHTTPService) ListWorkspace(context.Context, uuid.UUID, uuid.UUID) ([]*dto.Model, error) {
	return []*dto.Model{s.item()}, s.err
}

func (s *fakeModelHTTPService) ListPlatform(context.Context, uuid.UUID) ([]*dto.Model, error) {
	return []*dto.Model{s.item()}, s.err
}

func (s *fakeModelHTTPService) ListSelectableWorkspace(_ context.Context, _ uuid.UUID, modelType value.ModelType, active bool) ([]*dto.Model, error) {
	s.listSelectType, s.listSelectActive = modelType, active
	return []*dto.Model{s.item()}, s.err
}

func (s *fakeModelHTTPService) ListWorkspaceModels(_ context.Context, _ uuid.UUID, filter service.ModelListFilter) ([]*dto.Model, error) {
	s.listManaged, s.listFilter = true, filter
	return []*dto.Model{s.item()}, s.err
}

func (s *fakeModelHTTPService) ListPlatformModels(_ context.Context, filter service.ModelListFilter) ([]*dto.Model, error) {
	s.listManaged, s.listFilter = true, filter
	return []*dto.Model{s.item()}, s.err
}

func (s *fakeModelHTTPService) GetWorkspace(context.Context, uuid.UUID, uuid.UUID) (*dto.Model, error) {
	return s.item(), s.err
}

func (s *fakeModelHTTPService) GetPlatform(context.Context, uuid.UUID) (*dto.Model, error) {
	return s.item(), s.err
}

func (s *fakeModelHTTPService) UpdateWorkspace(_ context.Context, workspaceID, modelID uuid.UUID, input service.UpdateModelInput) (*dto.Model, error) {
	s.updateWorkspaceID, s.updateModelID, s.updateInput = workspaceID, modelID, input
	return s.item(), s.err
}

func (s *fakeModelHTTPService) UpdatePlatform(_ context.Context, modelID uuid.UUID, input service.UpdateModelInput) (*dto.Model, error) {
	s.updateModelID, s.updateInput = modelID, input
	return s.item(), s.err
}

func (s *fakeModelHTTPService) DeleteWorkspace(_ context.Context, workspaceID, modelID uuid.UUID) error {
	s.deleteWorkspaceID, s.deleteModelID = workspaceID, modelID
	return s.err
}

func (s *fakeModelHTTPService) DeletePlatform(_ context.Context, modelID uuid.UUID) error {
	s.deleteModelID = modelID
	return s.err
}

type fakeModelConnectionTestHTTPService struct {
	workspaceID uuid.UUID
	modelID     uuid.UUID
	result      *dto.ConnectionTestResult
	err         error
}

func (s *fakeModelConnectionTestHTTPService) TestWorkspace(_ context.Context, workspaceID, modelID uuid.UUID) (*dto.ConnectionTestResult, error) {
	s.workspaceID, s.modelID = workspaceID, modelID
	if s.result == nil {
		s.result = &dto.ConnectionTestResult{OK: true, Type: value.ModelTypeEmbedding, Dimensions: modelRoutesIntPtr(1024), DurationMS: 12}
	}
	return s.result, s.err
}

func (s *fakeModelConnectionTestHTTPService) TestPlatform(_ context.Context, modelID uuid.UUID) (*dto.ConnectionTestResult, error) {
	s.modelID = modelID
	if s.result == nil {
		s.result = &dto.ConnectionTestResult{OK: true, Type: value.ModelTypeEmbedding, Dimensions: modelRoutesIntPtr(1024), DurationMS: 12}
	}
	return s.result, s.err
}

type fakeModelKnowledgeBaseHTTPService struct{}

func (s *fakeModelKnowledgeBaseHTTPService) Create(_ context.Context, input service.CreateKnowledgeBaseInput) (*dto.KnowledgeBase, error) {
	return modelKnowledgeBaseDTO(input.WorkspaceID, uuid.New(), input.EmbeddingModelID), nil
}

func (s *fakeModelKnowledgeBaseHTTPService) Get(_ context.Context, access value.ResourceAccess, id uuid.UUID) (*dto.KnowledgeBase, error) {
	return modelKnowledgeBaseDTO(access.WorkspaceID, id, uuid.New()), nil
}

func (s *fakeModelKnowledgeBaseHTTPService) List(context.Context, value.ResourceAccess) ([]*dto.KnowledgeBase, error) {
	return []*dto.KnowledgeBase{}, nil
}

func (s *fakeModelKnowledgeBaseHTTPService) UpdateBasics(context.Context, service.UpdateKnowledgeBaseBasicsInput) (*dto.KnowledgeBase, error) {
	return nil, nil
}

func modelKnowledgeBaseDTO(workspaceID, knowledgeBaseID, modelID uuid.UUID) *dto.KnowledgeBase {
	return &dto.KnowledgeBase{
		ID: knowledgeBaseID, WorkspaceID: workspaceID, Name: "产品文档", EmbeddingModelID: modelID,
		EmbeddingModel: dto.EmbeddingModelSummary{ID: modelID, Name: "embedding", Provider: "openai", Dimensions: 1024, Available: true},
		ChunkingConfig: dto.ChunkingConfig{ChunkSize: 512, ChunkOverlap: 80}, Metadata: map[string]any{},
	}
}

type modelRouteFixture struct {
	router      stdhttp.Handler
	workspaceID uuid.UUID
	userID      uuid.UUID
	sessionID   uuid.UUID
	providers   *fakeModelProviderHTTPService
	models      *fakeModelHTTPService
	connections *fakeModelConnectionTestHTTPService
	knowledge   *fakeModelKnowledgeBaseHTTPService
}

func newModelRouteFixture(t *testing.T, role value.WorkspaceRole, platformAdmin bool) *modelRouteFixture {
	t.Helper()
	deps, auth, _, memberships, _ := newAuthTestDeps()
	workspaceID, userID, sessionID := uuid.New(), uuid.New(), uuid.New()
	auth.authUser = &model.User{ID: userID, IsPlatformAdmin: platformAdmin}
	auth.sessionID = sessionID
	memberships.getResult = &dto.Membership{ID: uuid.New(), WorkspaceID: workspaceID, UserID: userID, Role: role}
	workspaces := newFakeWorkspaceService()
	workspace := &dto.Workspace{ID: workspaceID, Name: "Acme", Slug: "acme", Metadata: map[string]any{}}
	workspaces.items[workspaceID] = workspace
	workspaces.slugIndex = map[string]*dto.Workspace{"acme": workspace}
	providers := &fakeModelProviderHTTPService{}
	models := &fakeModelHTTPService{}
	connections := &fakeModelConnectionTestHTTPService{}
	knowledge := &fakeModelKnowledgeBaseHTTPService{}
	deps.Workspaces = workspaces
	deps.KnowledgeBases = knowledge
	deps.ModelProviders = providers
	deps.Models = models
	deps.ModelConnectionTests = connections
	return &modelRouteFixture{
		router: NewRouter(deps), workspaceID: workspaceID, userID: userID, sessionID: sessionID,
		providers: providers, models: models, connections: connections, knowledge: knowledge,
	}
}

func (f *modelRouteFixture) request(method, path, body string) *httptest.ResponseRecorder {
	var input *bytes.Reader
	if body == "" {
		input = bytes.NewReader(nil)
	} else {
		input = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, input)
	req.AddCookie(&stdhttp.Cookie{Name: testCookieName, Value: f.sessionID.String()})
	if body != "" {
		req.Header.Set("content-type", "application/json")
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func validProviderJSON() string {
	return `{"name":"openai-prod","display_name":"OpenAI Production","description":"","provider":"openai","config":{"timeout_seconds":60},"credentials":{"api_key":"top-secret"}}`
}

func validModelJSON() string {
	return `{"name":"embedding-large","display_name":"Embedding Large","description":"","type":"embedding","model_name":"text-embedding-3-large","dimensions":1024,"parameters":{"batch_size":32}}`
}

func TestWorkspaceModelRoutesEnforceMemberAndAdminBoundaries(t *testing.T) {
	member := newModelRouteFixture(t, value.RoleMember, false)
	if rec := member.request(stdhttp.MethodGet, "/api/v1/workspaces/acme/model-providers", ""); rec.Code != stdhttp.StatusOK {
		t.Fatalf("member GET status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec := member.request(stdhttp.MethodPost, "/api/v1/workspaces/acme/model-providers", validProviderJSON()); rec.Code != stdhttp.StatusForbidden {
		t.Fatalf("member POST status = %d, want 403, body = %s", rec.Code, rec.Body.String())
	}

	admin := newModelRouteFixture(t, value.RoleAdmin, false)
	rec := admin.request(stdhttp.MethodPost, "/api/v1/workspaces/acme/model-providers", validProviderJSON())
	if rec.Code != stdhttp.StatusCreated {
		t.Fatalf("admin POST status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if admin.providers.createWorkspaceID != admin.workspaceID || admin.providers.createWorkspaceInput.ActorID != admin.userID {
		t.Fatalf("scope input = workspace %s actor %s", admin.providers.createWorkspaceID, admin.providers.createWorkspaceInput.ActorID)
	}
	if admin.providers.createWorkspaceInput.Scope != "" || admin.providers.createWorkspaceInput.WorkspaceID != nil {
		t.Fatalf("handler must not accept scope fields: %#v", admin.providers.createWorkspaceInput)
	}
}

func TestPlatformModelRoutesRequirePlatformAdmin(t *testing.T) {
	nonAdmin := newModelRouteFixture(t, value.RoleOwner, false)
	if rec := nonAdmin.request(stdhttp.MethodGet, "/api/v1/admin/model-providers", ""); rec.Code != stdhttp.StatusForbidden {
		t.Fatalf("non-admin status = %d, want 403", rec.Code)
	}

	admin := newModelRouteFixture(t, value.RoleMember, true)
	rec := admin.request(stdhttp.MethodPost, "/api/v1/admin/model-providers", validProviderJSON())
	if rec.Code != stdhttp.StatusCreated {
		t.Fatalf("platform create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if admin.providers.createPlatformInput.ActorID != admin.userID || admin.providers.createPlatformInput.Scope != "" || admin.providers.createPlatformInput.WorkspaceID != nil {
		t.Fatalf("platform input = %#v", admin.providers.createPlatformInput)
	}
}

func TestProviderResponseNeverContainsCredentialMaterialAndJSONIsStrict(t *testing.T) {
	f := newModelRouteFixture(t, value.RoleAdmin, false)
	rec := f.request(stdhttp.MethodPost, "/api/v1/workspaces/acme/model-providers", validProviderJSON())
	if rec.Code != stdhttp.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "top-secret") || strings.Contains(rec.Body.String(), `"api_key":`) {
		t.Fatalf("credential leaked: %s", rec.Body.String())
	}
	if got := string(f.providers.createWorkspaceInput.Credentials); !strings.Contains(got, "top-secret") {
		t.Fatalf("credentials not forwarded to service: %q", got)
	}

	before := f.providers.createCalls
	rec = f.request(stdhttp.MethodPost, "/api/v1/workspaces/acme/model-providers", strings.TrimSuffix(validProviderJSON(), "}")+`,"scope":"platform"}`)
	if rec.Code != stdhttp.StatusBadRequest || f.providers.createCalls != before {
		t.Fatalf("unknown field status = %d calls = %d body = %s", rec.Code, f.providers.createCalls, rec.Body.String())
	}
}

func TestProviderPatchDistinguishesMissingAndReplacementCredentials(t *testing.T) {
	providerID := uuid.New()
	f := newModelRouteFixture(t, value.RoleAdmin, false)
	rec := f.request(stdhttp.MethodPatch, "/api/v1/workspaces/acme/model-providers/"+providerID.String(), `{"display_name":"Renamed"}`)
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("missing credentials status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if f.providers.updateWorkspaceInput.Credentials != nil {
		t.Fatal("missing credentials must remain nil")
	}

	rec = f.request(stdhttp.MethodPatch, "/api/v1/workspaces/acme/model-providers/"+providerID.String(), `{"credentials":{"api_key":"replacement"}}`)
	if rec.Code != stdhttp.StatusOK || f.providers.updateWorkspaceInput.Credentials == nil {
		t.Fatalf("replacement credentials status = %d input = %#v", rec.Code, f.providers.updateWorkspaceInput)
	}
	if got := string(*f.providers.updateWorkspaceInput.Credentials); !strings.Contains(got, "replacement") {
		t.Fatalf("replacement = %s", got)
	}
}

func TestWorkspaceModelRoutesUsePathAndAuthContext(t *testing.T) {
	f := newModelRouteFixture(t, value.RoleOwner, false)
	providerID := uuid.New()
	rec := f.request(stdhttp.MethodPost, "/api/v1/workspaces/acme/model-providers/"+providerID.String()+"/models", validModelJSON())
	if rec.Code != stdhttp.StatusCreated {
		t.Fatalf("create model status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if f.models.createWorkspaceID != f.workspaceID || f.models.createInput.ProviderID != providerID || f.models.createInput.ActorID != f.userID {
		t.Fatalf("model input = %#v workspace = %s", f.models.createInput, f.models.createWorkspaceID)
	}

	modelID := uuid.New()
	rec = f.request(stdhttp.MethodPost, "/api/v1/workspaces/acme/models/"+modelID.String()+"/test", "")
	if rec.Code != stdhttp.StatusOK || f.connections.workspaceID != f.workspaceID || f.connections.modelID != modelID {
		t.Fatalf("test status = %d workspace = %s model = %s body = %s", rec.Code, f.connections.workspaceID, f.connections.modelID, rec.Body.String())
	}
}

func TestKnowledgeBaseEmbeddingModelUpdateRouteIsRemoved(t *testing.T) {
	kbID, modelID := uuid.New(), uuid.New()
	path := "/api/v1/workspaces/acme/knowledge-bases/" + kbID.String() + "/embedding-model"
	body := fmt.Sprintf(`{"embedding_model_id":%q}`, modelID.String())
	admin := newModelRouteFixture(t, value.RoleAdmin, false)
	rec := admin.request(stdhttp.MethodPatch, path, body)
	if rec.Code != stdhttp.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
}

func TestProviderOptionsExposeCapabilities(t *testing.T) {
	admin := newModelRouteFixture(t, value.RoleAdmin, false)
	rec := admin.request(stdhttp.MethodGet, "/api/v1/workspaces/acme/model-providers/options", "")
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	want := `{"providers":[{"key":"openai","capabilities":["embedding"],"model_catalog":false},{"key":"rerank_compatible","capabilities":["rerank"],"model_catalog":false}]}`
	if !jsonEqual(rec.Body.Bytes(), []byte(want)) {
		t.Fatalf("body = %s, want %s", rec.Body.String(), want)
	}
}

func TestProviderModelCatalogRoutesParseFiltersAndScope(t *testing.T) {
	workspace := newModelRouteFixture(t, value.RoleMember, false)
	providerID := uuid.New()
	rec := workspace.request(stdhttp.MethodGet, "/api/v1/workspaces/acme/model-providers/"+providerID.String()+"/model-catalog?type=rerank&q=BGE", "")
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("workspace status = %d body = %s", rec.Code, rec.Body.String())
	}
	if workspace.providers.catalogWorkspaceID != workspace.workspaceID || workspace.providers.catalogProviderID != providerID || workspace.providers.catalogFilter.Type == nil || *workspace.providers.catalogFilter.Type != value.ModelTypeRerank || workspace.providers.catalogFilter.Query != "BGE" {
		t.Fatalf("workspace catalog call = %#v", workspace.providers.catalogFilter)
	}

	platform := newModelRouteFixture(t, value.RoleMember, true)
	rec = platform.request(stdhttp.MethodGet, "/api/v1/admin/model-providers/"+providerID.String()+"/model-catalog?type=all", "")
	if rec.Code != stdhttp.StatusOK || platform.providers.catalogProviderID != providerID || platform.providers.catalogFilter.Type != nil {
		t.Fatalf("platform status = %d filter = %#v body = %s", rec.Code, platform.providers.catalogFilter, rec.Body.String())
	}

	bad := workspace.request(stdhttp.MethodGet, "/api/v1/workspaces/acme/model-providers/"+providerID.String()+"/model-catalog?type=llm", "")
	if bad.Code != stdhttp.StatusBadRequest || !strings.Contains(bad.Body.String(), "unsupported_model_type") {
		t.Fatalf("bad status = %d body = %s", bad.Code, bad.Body.String())
	}
}

func TestListSelectableModelsFiltersByType(t *testing.T) {
	admin := newModelRouteFixture(t, value.RoleAdmin, false)
	rec := admin.request(stdhttp.MethodGet, "/api/v1/workspaces/acme/models?type=rerank&active=true", "")
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if admin.models.listSelectType != value.ModelTypeRerank || !admin.models.listSelectActive {
		t.Fatalf("selectable call = type %s active %v", admin.models.listSelectType, admin.models.listSelectActive)
	}

	// 非 embedding/rerank 的 type 被拒绝。
	bad := admin.request(stdhttp.MethodGet, "/api/v1/workspaces/acme/models?type=llm", "")
	if bad.Code != stdhttp.StatusBadRequest {
		t.Fatalf("bad type status = %d, body = %s", bad.Code, bad.Body.String())
	}
}

func TestManagementModelCatalogRoutesParseFilters(t *testing.T) {
	workspace := newModelRouteFixture(t, value.RoleAdmin, false)
	rec := workspace.request(stdhttp.MethodGet, "/api/v1/workspaces/acme/models?management=true&type=rerank&status=active&scope=workspace&q=BGE", "")
	if rec.Code != stdhttp.StatusOK || !workspace.models.listManaged {
		t.Fatalf("workspace status = %d managed = %v body = %s", rec.Code, workspace.models.listManaged, rec.Body.String())
	}
	if workspace.models.listFilter.Type == nil || *workspace.models.listFilter.Type != value.ModelTypeRerank ||
		workspace.models.listFilter.Status == nil || *workspace.models.listFilter.Status != value.ModelStatusActive ||
		workspace.models.listFilter.Scope == nil || *workspace.models.listFilter.Scope != value.ModelScopeWorkspace ||
		workspace.models.listFilter.Query != "BGE" {
		t.Fatalf("filter = %#v", workspace.models.listFilter)
	}

	platform := newModelRouteFixture(t, value.RoleMember, true)
	rec = platform.request(stdhttp.MethodGet, "/api/v1/admin/models?type=all&status=disabled", "")
	if rec.Code != stdhttp.StatusOK || !platform.models.listManaged || platform.models.listFilter.Status == nil || *platform.models.listFilter.Status != value.ModelStatusDisabled {
		t.Fatalf("platform status = %d filter = %#v body = %s", rec.Code, platform.models.listFilter, rec.Body.String())
	}
}

func TestModelErrorMappingUsesStableSafeCodes(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"unsupported_provider", domainerrors.ErrUnsupportedProvider, 400, "unsupported_provider"},
		{"provider_scope_not_allowed", domainerrors.ErrProviderScopeNotAllowed, 400, "provider_scope_not_allowed"},
		{"invalid_provider_config", domainerrors.ErrInvalidProviderConfig, 400, "invalid_provider_config"},
		{"credentials_required", domainerrors.ErrCredentialsRequired, 400, "credentials_required"},
		{"unsupported_model_type", domainerrors.ErrUnsupportedModelType, 400, "unsupported_model_type"},
		{"unsupported_embedding_dimension", domainerrors.ErrUnsupportedEmbeddingDimension, 400, "unsupported_embedding_dimension"},
		{"model_not_visible", domainerrors.ErrModelNotVisible, 404, "model_not_visible"},
		{"model_disabled", domainerrors.ErrModelDisabled, 400, "model_disabled"},
		{"provider_disabled", domainerrors.ErrProviderDisabled, 400, "provider_disabled"},
		{"dimension_mismatch", domainerrors.ErrDimensionMismatch, 422, "dimension_mismatch"},
		{"connection_test_failed", domainerrors.ErrConnectionTestFailed, 502, "connection_test_failed"},
		{"authentication_failed", domainerrors.ErrAuthenticationFailed, 422, "authentication_failed"},
		{"endpoint_unreachable", domainerrors.ErrEndpointUnreachable, 502, "endpoint_unreachable"},
		{"request_timeout", domainerrors.ErrRequestTimeout, 504, "request_timeout"},
		{"rate_limited", domainerrors.ErrRateLimited, 429, "rate_limited"},
		{"provider_rejected", domainerrors.ErrProviderRejected, 502, "provider_rejected"},
		{"invalid_embedding_response", domainerrors.ErrInvalidEmbeddingResponse, 422, "invalid_embedding_response"},
		{"immutable_model_field", domainerrors.ErrImmutableModelField, 409, "immutable_model_field"},
		{"model_in_use", domainerrors.ErrModelInUse, 409, "model_in_use"},
		{"provider_in_use", domainerrors.ErrProviderInUse, 409, "provider_in_use"},
		{"rerank_configuration_conflict", domainerrors.ErrRerankConfigurationConflict, 409, "rerank_configuration_conflict"},
		{"rerank_snapshot_mismatch", domainerrors.ErrRerankSnapshotMismatch, 409, "rerank_snapshot_mismatch"},
		{"embedding_snapshot_mismatch", domainerrors.ErrEmbeddingSnapshotMismatch, 409, "embedding_snapshot_mismatch"},
		{"rerank_unavailable", domainerrors.ErrRerankUnavailable, 503, "rerank_unavailable"},
		{"rerank_rate_limited", domainerrors.ErrRerankRateLimited, 503, "rerank_rate_limited"},
		{"invalid_rerank_response", domainerrors.ErrInvalidRerankResponse, 502, "invalid_rerank_response"},
		{"rerank_input_too_large", domainerrors.ErrRerankInputTooLarge, 400, "rerank_input_too_large"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := NewRouter(Dependencies{})
			router.GET("/model-error", func(c *gin.Context) {
				writeServiceError(c, fmt.Errorf("upstream body must not leak: %w", test.err))
			})
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(stdhttp.MethodGet, "/model-error", nil))
			if rec.Code != test.status {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, test.status, rec.Body.String())
			}
			var body errorBody
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Error.Code != test.code || strings.Contains(rec.Body.String(), "upstream body") {
				t.Fatalf("body = %s, want code %s without upstream detail", rec.Body.String(), test.code)
			}
		})
	}
}

// modelRoutesIntPtr 返回 int 值的指针，便于构造 *int 类型的测试 DTO。
func modelRoutesIntPtr(value int) *int { return &value }

// jsonEqual 比较两段 JSON 字节是否语义相等（忽略键顺序）。
func jsonEqual(a, b []byte) bool {
	var va, vb any
	if err := json.Unmarshal(a, &va); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &vb); err != nil {
		return false
	}
	return reflect.DeepEqual(va, vb)
}
