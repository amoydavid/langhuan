package http

import (
	"bytes"
	"context"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

type fakeAssetGetter struct {
	asset *model.Asset
	err   error
}

func (f *fakeAssetGetter) GetByID(_ context.Context, workspaceID, assetID uuid.UUID) (*model.Asset, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.asset == nil || f.asset.WorkspaceID != workspaceID || f.asset.ID != assetID {
		return nil, domainerrors.ErrNotFound
	}
	return f.asset, nil
}

type fakeAssetContent struct {
	data []byte
	err  error
}

func (f *fakeAssetContent) Open(_ context.Context, _ string) (io.ReadCloser, error) {
	if f.err != nil {
		return nil, f.err
	}
	return io.NopCloser(bytes.NewReader(f.data)), nil
}

func testDocumentAssetRouter(t *testing.T, h documentHandler, authedWorkspaceID uuid.UUID) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	memberGroup := r.Group("/api/v1/workspaces/:workspace_slug")
	memberGroup.Use(func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{WorkspaceID: authedWorkspaceID})
		c.Next()
	})
	memberGroup.GET("/documents/:document_id/assets/:asset_id", h.assetContent)
	return r
}

func TestAssetContentProxyReturnsImage(t *testing.T) {
	workspaceID := uuid.New()
	documentID := uuid.New()
	assetID := uuid.New()
	asset := &model.Asset{
		ID:          assetID,
		WorkspaceID: workspaceID,
		DocumentID:  documentID,
		StorageKey:  "assets/ws/doc/rev/a.png",
		MimeType:    "image/png",
	}
	router := testDocumentAssetRouter(t, documentHandler{
		assetGetter:       &fakeAssetGetter{asset: asset},
		assetContentStore: &fakeAssetContent{data: []byte("fake png bytes")},
	}, workspaceID)

	req := httptest.NewRequest(stdhttp.MethodGet,
		"/api/v1/workspaces/acme/documents/"+documentID.String()+"/assets/"+assetID.String(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", rec.Header().Get("Content-Type"))
	}
	if rec.Body.String() != "fake png bytes" {
		t.Fatalf("body = %q, want image bytes", rec.Body.String())
	}
}

func TestAssetContentProxyRejectsCrossDocument(t *testing.T) {
	workspaceID := uuid.New()
	documentID := uuid.New()
	otherDocumentID := uuid.New()
	assetID := uuid.New()
	asset := &model.Asset{
		ID:          assetID,
		WorkspaceID: workspaceID,
		DocumentID:  documentID,
		StorageKey:  "assets/ws/doc/rev/a.png",
		MimeType:    "image/png",
	}
	router := testDocumentAssetRouter(t, documentHandler{
		assetGetter:       &fakeAssetGetter{asset: asset},
		assetContentStore: &fakeAssetContent{data: []byte("fake")},
	}, workspaceID)

	req := httptest.NewRequest(stdhttp.MethodGet,
		"/api/v1/workspaces/acme/documents/"+otherDocumentID.String()+"/assets/"+assetID.String(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusNotFound {
		t.Fatalf("status = %d, want 404 (cross-document)", rec.Code)
	}
}

func TestAssetContentProxyRejectsCrossWorkspace(t *testing.T) {
	assetWorkspaceID := uuid.New()
	authedWorkspaceID := uuid.New()
	assetID := uuid.New()
	asset := &model.Asset{
		ID:          assetID,
		WorkspaceID: assetWorkspaceID,
		StorageKey:  "assets/ws/doc/rev/a.png",
		MimeType:    "image/png",
	}
	router := testDocumentAssetRouter(t, documentHandler{
		assetGetter:       &fakeAssetGetter{asset: asset},
		assetContentStore: &fakeAssetContent{data: []byte("fake")},
	}, authedWorkspaceID)

	// auth 上下文 workspace 与 asset 的 workspace 不同
	req := httptest.NewRequest(stdhttp.MethodGet,
		"/api/v1/workspaces/acme/documents/"+uuid.New().String()+"/assets/"+assetID.String(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusNotFound {
		t.Fatalf("status = %d, want 404 (cross-workspace)", rec.Code)
	}
}

func TestAssetContentProxyRequiresServices(t *testing.T) {
	router := testDocumentAssetRouter(t, documentHandler{}, uuid.New())

	req := httptest.NewRequest(stdhttp.MethodGet,
		"/api/v1/workspaces/acme/documents/"+uuid.New().String()+"/assets/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusNotImplemented {
		t.Fatalf("status = %d, want 501 (services not enabled)", rec.Code)
	}
}
