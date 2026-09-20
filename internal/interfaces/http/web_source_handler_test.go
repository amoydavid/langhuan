package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
)

// fakeWebSourceService 是 WebSourceService 的测试替身：记录每次调用并返回预设
// 结果，不接触真实 DB 或入队器。
type fakeWebSourceService struct {
	createInput  *service.CreateWebSourceInput
	createSource *model.KbSource
	createJob    *model.Job
	createErr    error

	listViews []service.WebSourceView
	listErr   error

	getView *service.WebSourceView
	getErr  error

	updateInput  *service.UpdateWebSourceInput
	updateSource *model.KbSource
	updateJob    *model.Job
	updateErr    error

	syncCalls    int
	syncSourceID uuid.UUID
	syncGotForce bool
	syncJob      *model.Job
	syncErr      error

	deleteCalls    int
	deleteSourceID uuid.UUID
	deletePages    string
	deleteErr      error
}

func (f *fakeWebSourceService) CreateWebSource(_ context.Context, input service.CreateWebSourceInput) (*model.KbSource, *model.Job, error) {
	clone := input
	f.createInput = &clone
	if f.createErr != nil {
		return nil, nil, f.createErr
	}
	return f.createSource, f.createJob, nil
}

func (f *fakeWebSourceService) ListWebSources(_ context.Context, _, _ uuid.UUID) ([]service.WebSourceView, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listViews, nil
}

func (f *fakeWebSourceService) GetWebSource(_ context.Context, _, _, _ uuid.UUID) (*service.WebSourceView, error) {
	return f.getView, f.getErr
}

func (f *fakeWebSourceService) UpdateWebSource(_ context.Context, input service.UpdateWebSourceInput) (*model.KbSource, *model.Job, error) {
	clone := input
	f.updateInput = &clone
	if f.updateErr != nil {
		return nil, nil, f.updateErr
	}
	return f.updateSource, f.updateJob, nil
}

func (f *fakeWebSourceService) SyncWebSource(_ context.Context, _, _, sourceID uuid.UUID, force bool) (*model.Job, error) {
	f.syncCalls++
	f.syncSourceID = sourceID
	f.syncGotForce = force
	if f.syncErr != nil {
		return nil, f.syncErr
	}
	return f.syncJob, nil
}

func (f *fakeWebSourceService) DeleteWebSource(_ context.Context, _, _, sourceID uuid.UUID, pages string) error {
	f.deleteCalls++
	f.deleteSourceID = sourceID
	f.deletePages = pages
	return f.deleteErr
}

var _ WebSourceService = (*fakeWebSourceService)(nil)

// newWebSourceFixture 构造挂好 web-sources（和可选 source-connections）路由的
// router：user 是 acme workspace 的给定角色成员（Session 认证）。
func newWebSourceFixture(t *testing.T, role value.WorkspaceRole, svc *fakeWebSourceService, connSvc SourceConnectionService) (*gin.Engine, uuid.UUID, uuid.UUID) {
	t.Helper()
	workspaceID := uuid.New()
	userID := uuid.New()
	sessionID := uuid.New()
	authSvc := &fakeAuthService{
		authUser:  &model.User{ID: userID},
		sessionID: sessionID,
	}
	wsDTO := &dto.Workspace{ID: workspaceID, Name: "Acme", Slug: "acme", Metadata: map[string]any{}}
	wsSvc := &fakeWorkspaceService{
		items:     map[uuid.UUID]*dto.Workspace{workspaceID: wsDTO},
		slugIndex: map[string]*dto.Workspace{"acme": wsDTO},
	}
	mbs := &fakeMembershipService{getResult: &dto.Membership{ID: uuid.New(), WorkspaceID: workspaceID, UserID: userID, Role: role}}
	deps := Dependencies{
		Auth:          authSvc,
		SessionConfig: config.SessionConfig{CookieName: "langhuan_session"},
		PublicURLs:    mustPublicURLs(t),
		Workspaces:    wsSvc,
		Memberships:   mbs,
		WebSources:    svc,
	}
	if connSvc != nil {
		deps.SourceConnections = connSvc
	}
	return NewRouter(deps), workspaceID, sessionID
}

func webSourceRequest(router *gin.Engine, sessionID uuid.UUID, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "langhuan_session", Value: sessionID.String()})
	if body != "" {
		req.Header.Set("content-type", "application/json")
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// newKbSourceFixture 构造一个已填充字段的源领域对象。
func newKbSourceFixture(workspaceID, kbID uuid.UUID, name string) *model.KbSource {
	source, err := model.NewKbSource(model.NewKbSourceInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: kbID, Kind: value.KbSourceKindWeb,
		Name: name, Config: map[string]any{"mode": "urls", "urls": []any{"https://docs.example.com/a"}},
	})
	if err != nil {
		panic(err)
	}
	return source
}

func TestWebSourceHandlerAdminCreateReturns201WithJobID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	kbID := uuid.New()
	source := newKbSourceFixture(uuid.New(), kbID, "产品文档站")
	job := &model.Job{ID: uuid.New(), Type: model.SourceSyncJobType}
	svc := &fakeWebSourceService{createSource: source, createJob: job}
	router, _, sessionID := newWebSourceFixture(t, value.RoleAdmin, svc, nil)

	body := `{"name":"产品文档站","config":{"mode":"urls","urls":["https://docs.example.com/a"]}}`
	rec := webSourceRequest(router, sessionID, http.MethodPost,
		"/api/v1/workspaces/acme/knowledge-bases/"+kbID.String()+"/web-sources", body)
	require.Equal(t, http.StatusCreated, rec.Code)

	// workspace 来自 authContext、kb_id 来自路径参数，config 原样透传。
	require.NotNil(t, svc.createInput)
	require.Equal(t, kbID, svc.createInput.KnowledgeBaseID)
	require.Equal(t, "产品文档站", svc.createInput.Name)
	require.Equal(t, "urls", svc.createInput.Config["mode"])

	var resp struct {
		Source *dto.KbSource `json:"source"`
		JobID  *uuid.UUID    `json:"job_id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, source.ID, resp.Source.ID)
	require.Equal(t, "web", resp.Source.Kind)
	require.NotNil(t, resp.JobID)
	require.Equal(t, job.ID, *resp.JobID)
}

func TestWebSourceHandlerAdminCreateWithoutJobOmitsJobID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	kbID := uuid.New()
	source := newKbSourceFixture(uuid.New(), kbID, "s")
	svc := &fakeWebSourceService{createSource: source, createJob: nil}
	router, _, sessionID := newWebSourceFixture(t, value.RoleAdmin, svc, nil)

	rec := webSourceRequest(router, sessionID, http.MethodPost,
		"/api/v1/workspaces/acme/knowledge-bases/"+kbID.String()+"/web-sources", `{"name":"s"}`)
	require.Equal(t, http.StatusCreated, rec.Code)

	// 入队 best-effort：job 为 nil 时响应不得出现 job_id 键（而不是 null）。
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	_, has := raw["job_id"]
	require.False(t, has, "job 为 nil 时不应出现 job_id 键")
}

func TestWebSourceHandlerAdminCreateRejectsBadRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	kbID := uuid.New()
	svc := &fakeWebSourceService{createSource: newKbSourceFixture(uuid.New(), kbID, "s")}
	router, _, sessionID := newWebSourceFixture(t, value.RoleAdmin, svc, nil)

	tests := []struct {
		name string
		body string
	}{
		{name: "name 为空", body: `{"name":"   "}`},
		{name: "缺 name", body: `{"config":{"mode":"urls"}}`},
		{name: "未知字段", body: `{"name":"s","mode":"urls"}`},
		{name: "非 JSON", body: `{not json`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := webSourceRequest(router, sessionID, http.MethodPost,
				"/api/v1/workspaces/acme/knowledge-bases/"+kbID.String()+"/web-sources", tt.body)
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Nil(t, svc.createInput, "handler 拦截的请求不应到达 service")
		})
	}
}

// TestWebSourceHandlerAdminCreateMapsServiceErrors 断言领域错误经 writeServiceError
// 的标准映射：ErrValidation→400、ErrNotFound→404、ErrConflict→409。
func TestWebSourceHandlerAdminCreateMapsServiceErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	kbID := uuid.New()
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "校验失败（config/配额）", err: fmt.Errorf("%w: crawl.max_pages 超过天花板", domainerrors.ErrValidation), status: http.StatusBadRequest},
		{name: "知识库不存在", err: fmt.Errorf("读取知识库失败: %w", domainerrors.ErrNotFound), status: http.StatusNotFound},
		{name: "同名源冲突", err: fmt.Errorf("%w: 同名内容源已存在", domainerrors.ErrConflict), status: http.StatusConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeWebSourceService{createErr: tt.err}
			router, _, sessionID := newWebSourceFixture(t, value.RoleAdmin, svc, nil)
			rec := webSourceRequest(router, sessionID, http.MethodPost,
				"/api/v1/workspaces/acme/knowledge-bases/"+kbID.String()+"/web-sources", `{"name":"s"}`)
			require.Equal(t, tt.status, rec.Code)
		})
	}
}

// TestWebSourceHandlerRoleMatrix 固定权限矩阵：member 对源管理只读——GET 放行，
// 全部写操作被路由中间件拦为 403 且 service 不被调用；owner 可写。
func TestWebSourceHandlerRoleMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	kbID := uuid.New()
	sourceID := uuid.New()
	base := "/api/v1/workspaces/acme/knowledge-bases/" + kbID.String() + "/web-sources"

	t.Run("member 只读", func(t *testing.T) {
		svc := &fakeWebSourceService{getView: &service.WebSourceView{
			Source: newKbSourceFixture(uuid.New(), kbID, "s"),
		}}
		router, _, sessionID := newWebSourceFixture(t, value.RoleMember, svc, nil)

		rec := webSourceRequest(router, sessionID, http.MethodGet, base, "")
		require.Equal(t, http.StatusOK, rec.Code)
		rec = webSourceRequest(router, sessionID, http.MethodGet, base+"/"+sourceID.String(), "")
		require.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("member 写操作全部 403", func(t *testing.T) {
		svc := &fakeWebSourceService{}
		router, _, sessionID := newWebSourceFixture(t, value.RoleMember, svc, nil)

		requests := []struct {
			method, path, body string
		}{
			{http.MethodPost, base, `{"name":"s"}`},
			{http.MethodPatch, base + "/" + sourceID.String(), `{"name":"n"}`},
			{http.MethodPost, base + "/" + sourceID.String() + "/sync", `{}`},
			{http.MethodDelete, base + "/" + sourceID.String(), ""},
		}
		for _, r := range requests {
			rec := webSourceRequest(router, sessionID, r.method, r.path, r.body)
			require.Equal(t, http.StatusForbidden, rec.Code, "%s %s", r.method, r.path)
		}
		require.Equal(t, 0, svc.syncCalls)
		require.Equal(t, 0, svc.deleteCalls)
		require.Nil(t, svc.createInput)
		require.Nil(t, svc.updateInput)
	})

	t.Run("owner 可写", func(t *testing.T) {
		svc := &fakeWebSourceService{
			createSource: newKbSourceFixture(uuid.New(), kbID, "s"),
			updateSource: newKbSourceFixture(uuid.New(), kbID, "n"),
			syncJob:      &model.Job{ID: uuid.New()},
		}
		router, _, sessionID := newWebSourceFixture(t, value.RoleOwner, svc, nil)

		rec := webSourceRequest(router, sessionID, http.MethodPost, base, `{"name":"s"}`)
		require.Equal(t, http.StatusCreated, rec.Code)
		rec = webSourceRequest(router, sessionID, http.MethodPatch, base+"/"+sourceID.String(), `{"name":"n"}`)
		require.Equal(t, http.StatusOK, rec.Code)
		rec = webSourceRequest(router, sessionID, http.MethodPost, base+"/"+sourceID.String()+"/sync", "")
		require.Equal(t, http.StatusAccepted, rec.Code)
		rec = webSourceRequest(router, sessionID, http.MethodDelete, base+"/"+sourceID.String(), "")
		require.Equal(t, http.StatusNoContent, rec.Code)
	})
}

func TestWebSourceHandlerListReturnsPageCounts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	kbID := uuid.New()
	source := newKbSourceFixture(uuid.New(), kbID, "产品文档站")
	svc := &fakeWebSourceService{listViews: []service.WebSourceView{
		{Source: source, PageCount: 12, FailedPageCount: 2},
	}}
	router, _, sessionID := newWebSourceFixture(t, value.RoleAdmin, svc, nil)

	rec := webSourceRequest(router, sessionID, http.MethodGet,
		"/api/v1/workspaces/acme/knowledge-bases/"+kbID.String()+"/web-sources", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var got []struct {
		Source          *dto.KbSource `json:"source"`
		PageCount       int           `json:"page_count"`
		FailedPageCount int           `json:"failed_page_count"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got, 1)
	require.Equal(t, 12, got[0].PageCount)
	require.Equal(t, 2, got[0].FailedPageCount)
	require.Equal(t, source.ID, got[0].Source.ID)
	require.Equal(t, "active", got[0].Source.Status)
	require.Equal(t, "urls", got[0].Source.Config["mode"])
}

func TestWebSourceHandlerListEmptyReturnsBrackets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	kbID := uuid.New()
	svc := &fakeWebSourceService{}
	router, _, sessionID := newWebSourceFixture(t, value.RoleAdmin, svc, nil)

	rec := webSourceRequest(router, sessionID, http.MethodGet,
		"/api/v1/workspaces/acme/knowledge-bases/"+kbID.String()+"/web-sources", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "[]", strings.TrimSpace(rec.Body.String()))
}

func TestWebSourceHandlerGetNotFoundAndInvalidUUID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	kbID := uuid.New()
	svc := &fakeWebSourceService{getErr: fmt.Errorf("%w: 内容源不存在", domainerrors.ErrNotFound)}
	router, _, sessionID := newWebSourceFixture(t, value.RoleAdmin, svc, nil)

	rec := webSourceRequest(router, sessionID, http.MethodGet,
		"/api/v1/workspaces/acme/knowledge-bases/"+kbID.String()+"/web-sources/"+uuid.New().String(), "")
	require.Equal(t, http.StatusNotFound, rec.Code)

	rec = webSourceRequest(router, sessionID, http.MethodGet,
		"/api/v1/workspaces/acme/knowledge-bases/"+kbID.String()+"/web-sources/not-a-uuid", "")
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestWebSourceHandlerAdminUpdateForwardsKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	kbID := uuid.New()
	sourceID := uuid.New()
	source := newKbSourceFixture(uuid.New(), kbID, "renamed")
	job := &model.Job{ID: uuid.New()}
	svc := &fakeWebSourceService{updateSource: source, updateJob: job}
	router, _, sessionID := newWebSourceFixture(t, value.RoleAdmin, svc, nil)

	body := `{"name":"renamed","status":"disabled","config":{"sync":{"cron":"0 3 * * *"}}}`
	rec := webSourceRequest(router, sessionID, http.MethodPatch,
		"/api/v1/workspaces/acme/knowledge-bases/"+kbID.String()+"/web-sources/"+sourceID.String(), body)
	require.Equal(t, http.StatusOK, rec.Code)

	require.NotNil(t, svc.updateInput)
	require.Equal(t, kbID, svc.updateInput.KnowledgeBaseID)
	require.Equal(t, sourceID, svc.updateInput.SourceID)
	require.NotNil(t, svc.updateInput.Name)
	require.Equal(t, "renamed", *svc.updateInput.Name)
	require.NotNil(t, svc.updateInput.Status)
	require.Equal(t, value.KbSourceStatusDisabled, *svc.updateInput.Status)
	require.NotNil(t, svc.updateInput.Config)

	var resp struct {
		Source *dto.KbSource `json:"source"`
		JobID  *uuid.UUID    `json:"job_id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, source.ID, resp.Source.ID)
	require.NotNil(t, resp.JobID)
}

func TestWebSourceHandlerAdminUpdateRejectsBadRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	kbID := uuid.New()
	sourceID := uuid.New()
	svc := &fakeWebSourceService{updateSource: newKbSourceFixture(uuid.New(), kbID, "s")}
	router, _, sessionID := newWebSourceFixture(t, value.RoleAdmin, svc, nil)
	path := "/api/v1/workspaces/acme/knowledge-bases/" + kbID.String() + "/web-sources/" + sourceID.String()

	tests := []struct {
		name string
		body string
	}{
		{name: "空 body", body: `{}`},
		{name: "name 为空", body: `{"name":" "}`},
		{name: "非法 status", body: `{"status":"paused"}`},
		{name: "未知字段", body: `{"mode":"sitemap"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := webSourceRequest(router, sessionID, http.MethodPatch, path, tt.body)
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Nil(t, svc.updateInput, "handler 拦截的请求不应到达 service")
		})
	}
}

// TestWebSourceHandlerAdminUpdateMapsImmutableKeyRejection 断言 handler 不做键
// 白名单校验：不可变键/未知键由 service 报 ErrValidation 映射 400，文案透传。
func TestWebSourceHandlerAdminUpdateMapsImmutableKeyRejection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	kbID := uuid.New()
	svc := &fakeWebSourceService{updateErr: fmt.Errorf("%w: PATCH 键 %q 不允许修改", domainerrors.ErrValidation, "mode")}
	router, _, sessionID := newWebSourceFixture(t, value.RoleAdmin, svc, nil)

	rec := webSourceRequest(router, sessionID, http.MethodPatch,
		"/api/v1/workspaces/acme/knowledge-bases/"+kbID.String()+"/web-sources/"+uuid.New().String(),
		`{"config":{"mode":"sitemap"}}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.NotNil(t, svc.updateInput, "handler 不拦截 config 键，应透传给 service")
	require.Contains(t, rec.Body.String(), "不允许修改")
}

func TestWebSourceHandlerAdminSync(t *testing.T) {
	gin.SetMode(gin.TestMode)
	kbID := uuid.New()
	sourceID := uuid.New()
	base := "/api/v1/workspaces/acme/knowledge-bases/" + kbID.String() + "/web-sources/" + sourceID.String() + "/sync"

	t.Run("force body", func(t *testing.T) {
		svc := &fakeWebSourceService{syncJob: &model.Job{ID: uuid.New()}}
		router, _, sessionID := newWebSourceFixture(t, value.RoleAdmin, svc, nil)
		rec := webSourceRequest(router, sessionID, http.MethodPost, base, `{"force":true}`)
		require.Equal(t, http.StatusAccepted, rec.Code)
		require.Equal(t, 1, svc.syncCalls)
		require.Equal(t, sourceID, svc.syncSourceID)
		require.True(t, svc.syncGotForce)

		var resp struct {
			JobID *uuid.UUID `json:"job_id"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.NotNil(t, resp.JobID)
	})

	t.Run("空 body 视为普通同步", func(t *testing.T) {
		svc := &fakeWebSourceService{syncJob: &model.Job{ID: uuid.New()}}
		router, _, sessionID := newWebSourceFixture(t, value.RoleAdmin, svc, nil)
		rec := webSourceRequest(router, sessionID, http.MethodPost, base, "")
		require.Equal(t, http.StatusAccepted, rec.Code)
		require.False(t, svc.syncGotForce)
	})

	t.Run("未知字段拒绝", func(t *testing.T) {
		svc := &fakeWebSourceService{syncJob: &model.Job{ID: uuid.New()}}
		router, _, sessionID := newWebSourceFixture(t, value.RoleAdmin, svc, nil)
		rec := webSourceRequest(router, sessionID, http.MethodPost, base, `{"full":true}`)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Equal(t, 0, svc.syncCalls)
	})
}

func TestWebSourceHandlerAdminDelete(t *testing.T) {
	gin.SetMode(gin.TestMode)
	kbID := uuid.New()
	sourceID := uuid.New()
	base := "/api/v1/workspaces/acme/knowledge-bases/" + kbID.String() + "/web-sources/" + sourceID.String()

	t.Run("默认 keep", func(t *testing.T) {
		svc := &fakeWebSourceService{}
		router, _, sessionID := newWebSourceFixture(t, value.RoleAdmin, svc, nil)
		rec := webSourceRequest(router, sessionID, http.MethodDelete, base, "")
		require.Equal(t, http.StatusNoContent, rec.Code)
		require.Equal(t, "", svc.deletePages)
		require.Equal(t, sourceID, svc.deleteSourceID)
	})

	t.Run("remove 策略透传", func(t *testing.T) {
		svc := &fakeWebSourceService{}
		router, _, sessionID := newWebSourceFixture(t, value.RoleAdmin, svc, nil)
		rec := webSourceRequest(router, sessionID, http.MethodDelete, base+"?pages=remove", "")
		require.Equal(t, http.StatusNoContent, rec.Code)
		require.Equal(t, "remove", svc.deletePages)
	})

	t.Run("非法策略映射 400", func(t *testing.T) {
		svc := &fakeWebSourceService{deleteErr: fmt.Errorf("%w: 未知的 source delete policy %q", domainerrors.ErrValidation, "burn")}
		router, _, sessionID := newWebSourceFixture(t, value.RoleAdmin, svc, nil)
		rec := webSourceRequest(router, sessionID, http.MethodDelete, base+"?pages=burn", "")
		require.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("在飞同步守卫映射 409", func(t *testing.T) {
		svc := &fakeWebSourceService{deleteErr: fmt.Errorf("%w: 内容源有 1 个正在进行的同步任务，请稍后重试", domainerrors.ErrConflict)}
		router, _, sessionID := newWebSourceFixture(t, value.RoleAdmin, svc, nil)
		rec := webSourceRequest(router, sessionID, http.MethodDelete, base, "")
		require.Equal(t, http.StatusConflict, rec.Code)
	})

	t.Run("源不存在映射 404", func(t *testing.T) {
		svc := &fakeWebSourceService{deleteErr: domainerrors.ErrNotFound}
		router, _, sessionID := newWebSourceFixture(t, value.RoleAdmin, svc, nil)
		rec := webSourceRequest(router, sessionID, http.MethodDelete, base, "")
		require.Equal(t, http.StatusNotFound, rec.Code)
	})
}
