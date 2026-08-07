package http

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// idempotencyTextIngestFake is a standalone fake used by the idempotency handler
// tests. It does NOT collide with the legacy textIngestFake in
// document_text_handler_test.go because it has a different type name.
type idempotencyTextIngestFake struct {
	input  service.IngestDocumentInput
	result *service.IngestDocumentResult
	err    error
}

func (f *idempotencyTextIngestFake) Ingest(_ context.Context, input service.IngestDocumentInput) (*service.IngestDocumentResult, error) {
	f.input = input
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

// newBearerTestRouter wires a Bearer-authed document text ingest route against a
// fake ingest service. It mirrors the plan's helper signature.
func newBearerTestRouter(fake *idempotencyTextIngestFake) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/knowledge-bases/:id/documents/text", func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{
			PrincipalKind: value.PrincipalAPIKey,
			PrincipalID:   uuid.New(),
			WorkspaceID:   uuid.New(),
			Scopes:        []value.APIScope{value.ScopeDocumentsWrite},
		})
		documentHandler{ingestService: fake, maxFileSizeBytes: 1024}.ingestText(c)
	})
	return router
}

func postText(router http.Handler, key, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/knowledge-bases/"+uuid.NewString()+"/documents/text", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestIngestTextIdempotencyKeyIsForwarded(t *testing.T) {
	fake := &idempotencyTextIngestFake{result: &service.IngestDocumentResult{
		Document: &dto.Document{ID: uuid.New()},
		Job:      &dto.Job{ID: uuid.New()},
	}}
	router := newBearerTestRouter(fake)
	first := postText(router, "ticket-42-v1", `{"title":"排障","content":"#A","content_type":"markdown"}`)
	if first.Code != http.StatusCreated {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	if fake.input.IdempotencyKey != "ticket-42-v1" {
		t.Fatalf("key=%q", fake.input.IdempotencyKey)
	}
	if fake.input.CallerAPIKeyID == nil {
		t.Fatalf("CallerAPIKeyID not set for Bearer caller")
	}
}

func TestIngestTextIdempotencyKeyTooLongIs400(t *testing.T) {
	fake := &idempotencyTextIngestFake{result: &service.IngestDocumentResult{
		Document: &dto.Document{ID: uuid.New()},
	}}
	router := newBearerTestRouter(fake)
	longKey := make([]byte, 129)
	for i := range longKey {
		longKey[i] = 'a'
	}
	rec := postText(router, string(longKey), `{"title":"x","content":"y","content_type":"markdown"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if fake.input.IdempotencyKey != "" {
		t.Fatalf("oversized key reached service: %q", fake.input.IdempotencyKey)
	}
}

func TestIngestTextIdempotencyKeyMissingStaysBackwardCompatible(t *testing.T) {
	fake := &idempotencyTextIngestFake{result: &service.IngestDocumentResult{
		Document: &dto.Document{ID: uuid.New()},
	}}
	router := newBearerTestRouter(fake)
	rec := postText(router, "", `{"title":"x","content":"y","content_type":"markdown"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if fake.input.IdempotencyKey != "" {
		t.Fatalf("empty key should not be forwarded: %q", fake.input.IdempotencyKey)
	}
	if fake.input.CallerAPIKeyID == nil {
		t.Fatalf("CallerAPIKeyID should still be populated for Bearer")
	}
}

func TestIngestTextSameKeyBodyConflictIs409(t *testing.T) {
	// The E2E service is exercised through the service-layer test. At the handler
	// layer we assert the service error surfaces as a 409 conflict mapping.
	fake := &idempotencyTextIngestFake{err: domainerrors.ErrIdempotencyConflict}
	router := newBearerTestRouter(fake)
	rec := postText(router, "k-1", `{"title":"A","content":"two","content_type":"markdown"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
