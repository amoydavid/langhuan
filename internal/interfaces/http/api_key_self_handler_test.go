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
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestBearerJobStatusRejectsUnboundDocument(t *testing.T) {
	router, unboundJobID := newBearerJobRouter(t)
	rec := getBearerJob(router, unboundJobID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// newBearerJobRouter wires a jobHandler whose service returns a job bound to a
// KB that is NOT in the caller API key's allowed set. The handler relies on
// authCtx.ResourceAccess() to enforce the boundary; an unbound job must surface
// as 404.
func newBearerJobRouter(t *testing.T) (*gin.Engine, uuid.UUID) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	unboundJobID := uuid.New()
	callerAllowedKBs := []uuid.UUID{uuid.New()} // different KB; the job is bound elsewhere

	svc := &jobServiceFake{
		get: func(_ context.Context, access value.ResourceAccess, id uuid.UUID) (*dto.Job, error) {
			if id != unboundJobID {
				return nil, nil
			}
			job := &dto.Job{ID: unboundJobID, KnowledgeBaseID: uuid.New()}
			if !access.Unrestricted && !access.AllowsKnowledgeBase(job.KnowledgeBaseID) {
				return nil, errNotFoundSentinel
			}
			return job, nil
		},
	}
	router := gin.New()
	router.GET("/jobs/:id", func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{
			PrincipalKind:    value.PrincipalAPIKey,
			PrincipalID:      uuid.New(),
			WorkspaceID:      uuid.New(),
			Scopes:           []value.APIScope{value.ScopeDocumentsRead},
			KnowledgeBaseIDs: callerAllowedKBs,
		})
		jobHandler{service: svc}.get(c)
	})
	return router, unboundJobID
}

func getBearerJob(router http.Handler, jobID uuid.UUID) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/jobs/"+jobID.String(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestAPIKeySelfRejectsSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api-key/self", func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{
			PrincipalKind: value.PrincipalUser,
			PrincipalID:   uuid.New(),
			UserID:        uuid.New(),
		})
		apiKeySelfHandler{}.get(c)
	})
	req := httptest.NewRequest(http.MethodGet, "/api-key/self", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("session status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAPIKeySelfReturnsScopesForBearer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	scopes := []value.APIScope{value.ScopeDocumentsWrite, value.ScopeDocumentsRead}
	router := gin.New()
	router.GET("/api-key/self", func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{
			PrincipalKind: value.PrincipalAPIKey,
			PrincipalID:   uuid.New(),
			WorkspaceID:   uuid.New(),
			Scopes:        scopes,
		})
		apiKeySelfHandler{}.get(c)
	})
	req := httptest.NewRequest(http.MethodGet, "/api-key/self", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bearer status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, s := range scopes {
		if !strings.Contains(body, string(s)) {
			t.Fatalf("scope %q missing from response: %s", s, body)
		}
	}
	if strings.Contains(body, "lhk_") {
		t.Fatalf("response leaked token material: %s", body)
	}
}

// TestAPIKeySelfAllowsAnyBearerKey locks in the connection-test contract:
// /api-key/self exists so a downstream consumer (e.g. jinshu) can introspect a
// key's scopes to decide whether the key is sufficient. Requiring a specific
// scope here would be circular (the consumer must be able to detect that a key
// is MISSING exactly the scopes we'd otherwise require). Therefore any valid
// Bearer key — including one with zero scopes or only an unrelated scope — must
// be able to read its own scopes (200).
func TestAPIKeySelfAllowsAnyBearerKey(t *testing.T) {
	cases := []struct {
		name   string
		scopes []value.APIScope
	}{
		{"zero scopes", nil},
		{"zero scopes empty slice", []value.APIScope{}},
		{"only search read", []value.APIScope{value.ScopeSearchRead}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.GET("/api-key/self", func(c *gin.Context) {
				c.Set(authContextKey, value.AuthContext{
					PrincipalKind: value.PrincipalAPIKey,
					PrincipalID:   uuid.New(),
					WorkspaceID:   uuid.New(),
					Scopes:        tc.scopes,
				})
				apiKeySelfHandler{}.get(c)
			})
			req := httptest.NewRequest(http.MethodGet, "/api-key/self", nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s (must allow any valid Bearer key, no scope required)", rec.Code, rec.Body.String())
			}
		})
	}
}
