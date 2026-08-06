package http

import (
	"bytes"
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

type textIngestFake struct {
	input  *service.IngestDocumentInput
	result *service.IngestDocumentResult
}

func (f *textIngestFake) Ingest(_ context.Context, input service.IngestDocumentInput) (*service.IngestDocumentResult, error) {
	f.input = &input
	return f.result, nil
}

func TestDocumentTextHandlerMapsMarkdownRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspaceID, knowledgeBaseID, parentID := uuid.New(), uuid.New(), uuid.New()
	fake := &textIngestFake{result: &service.IngestDocumentResult{Document: &dto.Document{ID: uuid.New()}}}
	router := gin.New()
	router.POST("/knowledge-bases/:id/documents/text", func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{WorkspaceID: workspaceID, UserID: uuid.New()})
		documentHandler{ingestService: fake, maxFileSizeBytes: 1024}.ingestText(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/knowledge-bases/"+knowledgeBaseID.String()+"/documents/text", bytes.NewBufferString(`{"title":"排障","content":"# 标题\n\n正文","content_type":"markdown","parent_node_id":"`+parentID.String()+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status/body = %d %s", rec.Code, rec.Body.String())
	}
	if fake.input == nil || fake.input.Title != "排障" || fake.input.FileName != "排障.md" || fake.input.ContentType != "text/markdown" || fake.input.SourceType != "api" || fake.input.Dedupe || fake.input.ParentNodeID == nil || *fake.input.ParentNodeID != parentID || fake.input.SizeBytes != int64(len([]byte("# 标题\n\n正文"))) {
		t.Fatalf("ingest input = %#v", fake.input)
	}
}

func TestDocumentTextHandlerRejectsHTMLAndOversize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &textIngestFake{}
	router := gin.New()
	router.POST("/knowledge-bases/:id/documents/text", func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{WorkspaceID: uuid.New(), UserID: uuid.New()})
		documentHandler{ingestService: fake, maxFileSizeBytes: 4}.ingestText(c)
	})
	for name, body := range map[string]string{
		"html":  `{"title":"x","content":"<h1>x</h1>","content_type":"html"}`,
		"large": `{"title":"x","content":"12345","content_type":"markdown"}`,
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/knowledge-bases/"+uuid.NewString()+"/documents/text", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"code":"validation_error"`) {
				t.Fatalf("status/body = %d %s", rec.Code, rec.Body.String())
			}
		})
	}
	if fake.input != nil {
		t.Fatalf("invalid request reached ingest: %#v", fake.input)
	}
}
