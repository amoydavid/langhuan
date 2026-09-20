package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
)

type documentListFake struct {
	filter *service.DocumentListFilter
	items  []*dto.Document
}

func (f *documentListFake) List(_ context.Context, filter service.DocumentListFilter) ([]*dto.Document, error) {
	f.filter = &filter
	return f.items, nil
}

func (f *documentListFake) Get(_ context.Context, _ value.ResourceAccess, _ uuid.UUID) (*dto.Document, error) {
	return nil, nil
}

func (f *documentListFake) Delete(_ context.Context, _ value.ResourceAccess, _ uuid.UUID) error {
	return nil
}

// TestDocumentListHandlerParsesKbSourceIDFilter 验证源详情页依赖的
// kb_source_id 过滤参数（v1.3.0 spec §9.4）：合法 UUID 下推到 service，
// 非法 UUID 拒绝 400 且不触达 service。
func TestDocumentListHandlerParsesKbSourceIDFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspaceID, kbID, sourceID := uuid.New(), uuid.New(), uuid.New()
	fake := &documentListFake{items: []*dto.Document{}}
	router := gin.New()
	router.GET("/knowledge-bases/:id/documents", func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{WorkspaceID: workspaceID, UserID: uuid.New()})
		documentHandler{queryService: fake}.list(c)
	})

	req := httptest.NewRequest(http.MethodGet,
		"/knowledge-bases/"+kbID.String()+"/documents?kind=web&kb_source_id="+sourceID.String(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if fake.filter == nil || fake.filter.Kind == nil || *fake.filter.Kind != value.DocumentKindWeb ||
		fake.filter.KbSourceID == nil || *fake.filter.KbSourceID != sourceID ||
		fake.filter.WorkspaceID != workspaceID || fake.filter.KnowledgeBaseID != kbID {
		t.Fatalf("filter = %#v", fake.filter)
	}
}

func TestDocumentListHandlerRejectsInvalidKbSourceID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &documentListFake{}
	router := gin.New()
	router.GET("/knowledge-bases/:id/documents", func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{WorkspaceID: uuid.New(), UserID: uuid.New()})
		documentHandler{queryService: fake}.list(c)
	})
	req := httptest.NewRequest(http.MethodGet,
		"/knowledge-bases/"+uuid.NewString()+"/documents?kb_source_id=not-a-uuid", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"code":"validation_error"`) {
		t.Fatalf("status/body = %d %s", rec.Code, rec.Body.String())
	}
	if fake.filter != nil {
		t.Fatalf("invalid kb_source_id reached service: %#v", fake.filter)
	}
}
