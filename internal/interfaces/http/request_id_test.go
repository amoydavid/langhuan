package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/requestmeta"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestRequestIDMiddlewareAcceptsValidAndReplacesInvalid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantSet bool
	}{
		{"valid id", "valid-id:42", true},
		{"invalid spaces and slash", "contains spaces and / slash", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(RequestID())
			router.GET("/", func(c *gin.Context) {
				meta := requestmeta.From(c.Request.Context())
				c.String(http.StatusOK, meta.RequestID)
			})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.input != "" {
				req.Header.Set(requestIDHeader, tt.input)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			headerID := rec.Header().Get(requestIDHeader)
			bodyID := rec.Body.String()
			if headerID != bodyID {
				t.Fatalf("header %q != body %q", headerID, bodyID)
			}
			if tt.wantSet {
				if headerID != tt.input {
					t.Fatalf("want %q, got %q", tt.input, headerID)
				}
			} else {
				if _, err := uuid.Parse(headerID); err != nil {
					t.Fatalf("invalid id replaced with non-UUID %q", headerID)
				}
			}
		})
	}
}

func TestRequestIDSetsRestTransport(t *testing.T) {
	t.Parallel()
	router := gin.New()
	router.Use(RequestID())
	router.GET("/", func(c *gin.Context) {
		meta := requestmeta.From(c.Request.Context())
		c.JSON(http.StatusOK, meta)
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Body.String() == "" {
		t.Fatal("empty body")
	}
	// 简单断言 transport=rest 出现在 JSON 中。
	if !contains(rec.Body.String(), `"Transport":"rest"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestMarkConsoleSearchOverridesTransportForSessionPrincipal(t *testing.T) {
	t.Parallel()
	newRouter := func(auth value.AuthContext) *gin.Engine {
		router := gin.New()
		router.Use(func(c *gin.Context) {
			if auth.PrincipalKind != "" {
				c.Set("auth", auth)
			}
			c.Next()
		}, RequestID(), func(c *gin.Context) {
			markConsoleSearch(c)
			meta := requestmeta.From(c.Request.Context())
			c.JSON(http.StatusOK, meta)
		})
		router.GET("/", func(c *gin.Context) {})
		return router
	}

	// Session principal + 标记头 → transport=console。
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(consoleSearchHeader, "1")
	newRouter(value.AuthContext{UserID: uuid.New(), PrincipalKind: value.PrincipalUser}).ServeHTTP(rec, req)
	if !contains(rec.Body.String(), `"Transport":"console"`) {
		t.Fatalf("session + console header 应覆盖 transport=console，body = %s", rec.Body.String())
	}

	// API Key principal 携带该头 → 忽略，保持 rest。
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(consoleSearchHeader, "1")
	newRouter(value.AuthContext{PrincipalKind: value.PrincipalAPIKey}).ServeHTTP(rec, req)
	if !contains(rec.Body.String(), `"Transport":"rest"`) {
		t.Fatalf("api_key 携带 console header 必须被忽略，body = %s", rec.Body.String())
	}

	// 无标记头 → 保持 rest。
	rec = httptest.NewRecorder()
	newRouter(value.AuthContext{UserID: uuid.New(), PrincipalKind: value.PrincipalUser}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !contains(rec.Body.String(), `"Transport":"rest"`) {
		t.Fatalf("无 header 必须保持 rest，body = %s", rec.Body.String())
	}
}
