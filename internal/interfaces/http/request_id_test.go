package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/requestmeta"
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
