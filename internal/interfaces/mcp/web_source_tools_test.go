package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// 本文件覆盖 web_source_* 工具组：list/get 输出投影（mode 提取、页面计数）、
// create/update 输入构造与 job 可选、sync force 透传、delete pages 校验与默认、
// scope/KB 绑定拒绝。

// fakeWebSourceManageService 记录 web 源管理端口的调用。
type fakeWebSourceManageService struct {
	listCalls   int
	listKBID    uuid.UUID
	getCalls    int
	getIDs      [2]uuid.UUID
	createCalls int
	createInput service.CreateWebSourceInput
	updateCalls int
	updateInput service.UpdateWebSourceInput
	syncCalls   int
	syncArgs    struct {
		kbID, sourceID uuid.UUID
		force          bool
	}
	deleteCalls int
	deleteArgs  struct {
		kbID, sourceID uuid.UUID
		pages          string
	}

	views     []service.WebSourceView
	view      *service.WebSourceView
	created   *model.KbSource
	job       *model.Job
	updateErr error
	createErr error
	syncErr   error
	deleteErr error
}

func (f *fakeWebSourceManageService) AddURLToWebSource(context.Context, uuid.UUID, uuid.UUID, string) (*model.KbSource, *model.Job, error) {
	return nil, nil, fmt.Errorf("not used in web_source tests")
}

func (f *fakeWebSourceManageService) CreateWebSource(_ context.Context, input service.CreateWebSourceInput) (*model.KbSource, *model.Job, error) {
	f.createCalls++
	f.createInput = input
	if f.createErr != nil {
		return nil, nil, f.createErr
	}
	return f.created, f.job, nil
}

func (f *fakeWebSourceManageService) ListWebSources(_ context.Context, _ value.ResourceAccess, kbID uuid.UUID) ([]service.WebSourceView, error) {
	f.listCalls++
	f.listKBID = kbID
	return f.views, nil
}

func (f *fakeWebSourceManageService) GetWebSource(_ context.Context, _ value.ResourceAccess, kbID, sourceID uuid.UUID) (*service.WebSourceView, error) {
	f.getCalls++
	f.getIDs = [2]uuid.UUID{kbID, sourceID}
	return f.view, nil
}

func (f *fakeWebSourceManageService) UpdateWebSource(_ context.Context, input service.UpdateWebSourceInput) (*model.KbSource, *model.Job, error) {
	f.updateCalls++
	f.updateInput = input
	if f.updateErr != nil {
		return nil, nil, f.updateErr
	}
	return f.created, f.job, nil
}

func (f *fakeWebSourceManageService) SyncWebSource(_ context.Context, workspaceID, kbID, sourceID uuid.UUID, force bool) (*model.Job, error) {
	f.syncCalls++
	f.syncArgs.kbID, f.syncArgs.sourceID, f.syncArgs.force = kbID, sourceID, force
	if f.syncErr != nil {
		return nil, f.syncErr
	}
	return f.job, nil
}

func (f *fakeWebSourceManageService) DeleteWebSource(_ context.Context, workspaceID, kbID, sourceID uuid.UUID, pages string) error {
	f.deleteCalls++
	f.deleteArgs.kbID, f.deleteArgs.sourceID, f.deleteArgs.pages = kbID, sourceID, pages
	return f.deleteErr
}

func webSourceTestDeps(fake MCPWebSourceService) Dependencies {
	return Dependencies{WebSources: fake, InlineLimit: 8 * 1024 * 1024}
}

func callWebSourceTool(t *testing.T, deps Dependencies, auth value.AuthContext, tool, args string) *mcplib.CallToolResult {
	t.Helper()
	srv := NewServer(deps)
	payload := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, tool, args)
	ctx := value.ContextWithAuthContext(context.Background(), auth)
	resp := srv.MCP().HandleMessage(ctx, json.RawMessage(payload))
	jsonResp, ok := resp.(mcplib.JSONRPCResponse)
	require.True(t, ok, "response type = %T", resp)
	result, ok := jsonResp.Result.(*mcplib.CallToolResult)
	require.True(t, ok, "result type = %T", jsonResp.Result)
	return result
}

func webSourceAuth(kbID uuid.UUID, scopes ...value.APIScope) value.AuthContext {
	if scopes == nil {
		scopes = []value.APIScope{value.ScopeKnowledgeBasesRead, value.ScopeKnowledgeBasesWrite}
	}
	return value.NewAPIKeyAuthContext(uuid.New(), uuid.New(), scopes, []uuid.UUID{kbID})
}

func webSourceFixture() *model.KbSource {
	return &model.KbSource{
		ID: uuid.New(), Name: "官方文档", Status: value.KbSourceStatusActive,
		Config: map[string]any{"mode": "urls", "urls": []any{"https://example.com/a"}},
	}
}

func TestWebSourceListProjection(t *testing.T) {
	kbID := uuid.New()
	fake := &fakeWebSourceManageService{views: []service.WebSourceView{
		{Source: webSourceFixture(), PageCount: 3, FailedPageCount: 1},
	}}

	result := callWebSourceTool(t, webSourceTestDeps(fake), webSourceAuth(kbID), "web_source_list",
		fmt.Sprintf(`{"knowledge_base_id":%q}`, kbID))
	out := structuredOf(t, result)
	sources := out["sources"].([]any)
	require.Len(t, sources, 1)
	item := sources[0].(map[string]any)
	require.Equal(t, "官方文档", item["name"])
	require.Equal(t, "urls", item["mode"])
	require.Equal(t, float64(3), item["page_count"])
	require.Equal(t, float64(1), item["failed_page_count"])
	require.Equal(t, kbID, fake.listKBID)
}

func TestWebSourceGetDetail(t *testing.T) {
	kbID := uuid.New()
	source := webSourceFixture()
	fake := &fakeWebSourceManageService{view: &service.WebSourceView{Source: source, PageCount: 2}}

	result := callWebSourceTool(t, webSourceTestDeps(fake), webSourceAuth(kbID), "web_source_get",
		fmt.Sprintf(`{"knowledge_base_id":%q,"source_id":%q}`, kbID, source.ID))
	out := structuredOf(t, result)
	require.Equal(t, source.ID.String(), out["id"])
	require.Equal(t, "urls", out["mode"])
	config := out["config"].(map[string]any)
	require.Equal(t, "urls", config["mode"])
	require.Equal(t, [2]uuid.UUID{kbID, source.ID}, fake.getIDs)
}

func TestWebSourceCreateInputAndOptionalJob(t *testing.T) {
	kbID := uuid.New()
	fake := &fakeWebSourceManageService{created: webSourceFixture()}

	// 无 job：job_id 省略。
	result := callWebSourceTool(t, webSourceTestDeps(fake), webSourceAuth(kbID), "web_source_create", fmt.Sprintf(`{
		"knowledge_base_id":%q,"name":"官方文档",
		"config":{"mode":"urls","urls":["https://example.com/a"]}
	}`, kbID))
	out := structuredOf(t, result)
	require.Equal(t, fake.created.ID.String(), out["source_id"])
	require.Equal(t, "active", out["status"])
	require.Equal(t, "urls", out["mode"])
	require.NotContains(t, out, "job_id")

	require.Equal(t, 1, fake.createCalls)
	require.Equal(t, kbID, fake.createInput.KnowledgeBaseID)
	require.Equal(t, "官方文档", fake.createInput.Name)
	require.Equal(t, "urls", fake.createInput.Config["mode"])

	// 有 job：输出 job_id。
	fake.job = &model.Job{ID: uuid.New()}
	out = structuredOf(t, callWebSourceTool(t, webSourceTestDeps(fake), webSourceAuth(kbID), "web_source_create", fmt.Sprintf(`{
		"knowledge_base_id":%q,"name":"官方文档","config":{"mode":"urls","urls":[]}
	}`, kbID)))
	require.Equal(t, fake.job.ID.String(), out["job_id"])
}

func TestWebSourceUpdateValidations(t *testing.T) {
	kbID, sourceID := uuid.New(), uuid.New()
	fake := &fakeWebSourceManageService{created: webSourceFixture()}

	// 全空拒绝。
	parsed := errorStructuredOf(t, callWebSourceTool(t, webSourceTestDeps(fake), webSourceAuth(kbID), "web_source_update",
		fmt.Sprintf(`{"knowledge_base_id":%q,"source_id":%q}`, kbID, sourceID)))
	require.Equal(t, "validation_error", parsed.Error.Code)

	// 非法 status。
	parsed = errorStructuredOf(t, callWebSourceTool(t, webSourceTestDeps(fake), webSourceAuth(kbID), "web_source_update",
		fmt.Sprintf(`{"knowledge_base_id":%q,"source_id":%q,"status":"paused"}`, kbID, sourceID)))
	require.Equal(t, "validation_error", parsed.Error.Code)
	require.Zero(t, fake.updateCalls)

	// 合法改名 + 停用。
	disabled := value.KbSourceStatusDisabled
	structuredOf(t, callWebSourceTool(t, webSourceTestDeps(fake), webSourceAuth(kbID), "web_source_update", fmt.Sprintf(`{
		"knowledge_base_id":%q,"source_id":%q,"name":"改名","status":"disabled"
	}`, kbID, sourceID)))
	require.Equal(t, 1, fake.updateCalls)
	require.NotNil(t, fake.updateInput.Name)
	require.Equal(t, "改名", *fake.updateInput.Name)
	require.NotNil(t, fake.updateInput.Status)
	require.Equal(t, disabled, *fake.updateInput.Status)

	// config 校验错误透传（ParseWebSourceConfig 走 ErrValidation）。
	fake.updateErr = fmt.Errorf("%w: 未知配置键", domainerrors.ErrValidation)
	parsed = errorStructuredOf(t, callWebSourceTool(t, webSourceTestDeps(fake), webSourceAuth(kbID), "web_source_update",
		fmt.Sprintf(`{"knowledge_base_id":%q,"source_id":%q,"config":{"oops":1}}`, kbID, sourceID)))
	require.Equal(t, "validation_error", parsed.Error.Code)
}

func TestWebSourceSyncForcePassthrough(t *testing.T) {
	kbID, sourceID := uuid.New(), uuid.New()
	job := &model.Job{ID: uuid.New()}
	fake := &fakeWebSourceManageService{job: job}

	out := structuredOf(t, callWebSourceTool(t, webSourceTestDeps(fake), webSourceAuth(kbID), "web_source_sync",
		fmt.Sprintf(`{"knowledge_base_id":%q,"source_id":%q,"force":true}`, kbID, sourceID)))
	require.Equal(t, job.ID.String(), out["job_id"])
	require.Equal(t, 1, fake.syncCalls)
	require.True(t, fake.syncArgs.force)

	// force 缺省 false。
	structuredOf(t, callWebSourceTool(t, webSourceTestDeps(fake), webSourceAuth(kbID), "web_source_sync",
		fmt.Sprintf(`{"knowledge_base_id":%q,"source_id":%q}`, kbID, sourceID)))
	require.False(t, fake.syncArgs.force)
}

func TestWebSourceDeletePagesValidation(t *testing.T) {
	kbID, sourceID := uuid.New(), uuid.New()
	fake := &fakeWebSourceManageService{}

	parsed := errorStructuredOf(t, callWebSourceTool(t, webSourceTestDeps(fake), webSourceAuth(kbID), "web_source_delete",
		fmt.Sprintf(`{"knowledge_base_id":%q,"source_id":%q,"pages":"nuke"}`, kbID, sourceID)))
	require.Equal(t, "validation_error", parsed.Error.Code)
	require.Zero(t, fake.deleteCalls)

	out := structuredOf(t, callWebSourceTool(t, webSourceTestDeps(fake), webSourceAuth(kbID), "web_source_delete",
		fmt.Sprintf(`{"knowledge_base_id":%q,"source_id":%q}`, kbID, sourceID)))
	require.Equal(t, true, out["deleted"])
	require.Equal(t, "keep", fake.deleteArgs.pages) // 缺省 keep

	structuredOf(t, callWebSourceTool(t, webSourceTestDeps(fake), webSourceAuth(kbID), "web_source_delete",
		fmt.Sprintf(`{"knowledge_base_id":%q,"source_id":%q,"pages":"remove"}`, kbID, sourceID)))
	require.Equal(t, "remove", fake.deleteArgs.pages)
}

func TestWebSourceScopeMatrix(t *testing.T) {
	kbID, sourceID := uuid.New(), uuid.New()
	fake := &fakeWebSourceManageService{views: []service.WebSourceView{}}
	// 只有 documents scope：全部 web_source 工具均拒绝。注意输入校验先于
	// handler 执行，每个工具的参数必须先通过各自 schema，才能到达 scope 检查。
	docOnly := webSourceAuth(kbID, value.ScopeDocumentsWrite, value.ScopeDocumentsRead)
	argsByTool := map[string]string{
		"web_source_list":   fmt.Sprintf(`{"knowledge_base_id":%q}`, kbID),
		"web_source_get":    fmt.Sprintf(`{"knowledge_base_id":%q,"source_id":%q}`, kbID, sourceID),
		"web_source_create": fmt.Sprintf(`{"knowledge_base_id":%q,"name":"s","config":{"mode":"urls","urls":[]}}`, kbID),
		"web_source_update": fmt.Sprintf(`{"knowledge_base_id":%q,"source_id":%q,"name":"s"}`, kbID, sourceID),
		"web_source_sync":   fmt.Sprintf(`{"knowledge_base_id":%q,"source_id":%q}`, kbID, sourceID),
		"web_source_delete": fmt.Sprintf(`{"knowledge_base_id":%q,"source_id":%q}`, kbID, sourceID),
	}
	for tool, args := range argsByTool {
		assertToolFilterDenial(t, callMCPToolRaw(webSourceTestDeps(fake), docOnly, tool, args))
	}

	// KB 绑定外：not_found。
	unbound := webSourceAuth(uuid.New())
	parsed := errorStructuredOf(t, callWebSourceTool(t, webSourceTestDeps(fake), unbound, "web_source_list",
		fmt.Sprintf(`{"knowledge_base_id":%q}`, kbID)))
	require.Equal(t, "not_found", parsed.Error.Code)
}
