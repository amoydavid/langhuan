package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestEmbeddedSPAServesBundleAndFallback(t *testing.T) {
	bundle := fstest.MapFS{
		"index.html":    {Data: []byte("<html>console</html>")},
		"assets/app.js": {Data: []byte("console.log('langhuan')")},
	}
	router := NewRouter(Dependencies{SPA: bundle})

	tests := []struct {
		name            string
		method          string
		target          string
		wantStatus      int
		wantBody        string
		wantContentType string
	}{
		{
			name:            "root",
			method:          http.MethodGet,
			target:          "/",
			wantStatus:      http.StatusOK,
			wantBody:        "<html>console</html>",
			wantContentType: "text/html",
		},
		{
			name:            "asset",
			method:          http.MethodGet,
			target:          "/assets/app.js",
			wantStatus:      http.StatusOK,
			wantBody:        "console.log('langhuan')",
			wantContentType: "text/javascript",
		},
		{
			name:            "deep route",
			method:          http.MethodGet,
			target:          "/workspaces/demo/kb/example",
			wantStatus:      http.StatusOK,
			wantBody:        "<html>console</html>",
			wantContentType: "text/html",
		},
		{
			name:            "invitation route",
			method:          http.MethodGet,
			target:          "/invitations/token",
			wantStatus:      http.StatusOK,
			wantBody:        "<html>console</html>",
			wantContentType: "text/html",
		},
		{
			name:            "directory path falls back",
			method:          http.MethodGet,
			target:          "/assets",
			wantStatus:      http.StatusOK,
			wantBody:        "<html>console</html>",
			wantContentType: "text/html",
		},
		{
			name:            "head",
			method:          http.MethodHead,
			target:          "/assets/app.js",
			wantStatus:      http.StatusOK,
			wantContentType: "text/javascript",
		},
		{
			name:       "write method",
			method:     http.MethodPost,
			target:     "/workspaces/demo",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "traversal",
			method:     http.MethodGet,
			target:     "/../secret",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "repeated separator",
			method:     http.MethodGet,
			target:     "/assets//app.js",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, tt.target, nil)

			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %q", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantBody != "" && rec.Body.String() != tt.wantBody {
				t.Fatalf("body = %q, want %q", rec.Body.String(), tt.wantBody)
			}
			if tt.wantContentType != "" && !strings.HasPrefix(rec.Header().Get("Content-Type"), tt.wantContentType) {
				t.Fatalf("content-type = %q, want prefix %q", rec.Header().Get("Content-Type"), tt.wantContentType)
			}
		})
	}
}

func TestEmbeddedSPAServesV050CanonicalLeafRoutes(t *testing.T) {
	bundle := fstest.MapFS{
		"index.html": {Data: []byte("<html>console</html>")},
	}
	router := NewRouter(Dependencies{SPA: bundle})

	routes := []string{
		"/workspaces/acme/kb/product-docs",
		"/workspaces/acme/kb/product-docs/content/all",
		"/workspaces/acme/kb/product-docs/content/files",
		"/workspaces/acme/kb/product-docs/content/files/document-id",
		"/workspaces/acme/kb/product-docs/content/files/document-id?chunk=chunk-id",
		"/workspaces/acme/kb/product-docs/content/faq",
		"/workspaces/acme/kb/product-docs/content/faq/new",
		"/workspaces/acme/kb/product-docs/content/faq/document-id",
		"/workspaces/acme/kb/product-docs/content/faq/document-id/edit",
		"/workspaces/acme/kb/product-docs/content/web",
		"/workspaces/acme/kb/product-docs/content/web/document-id",
		"/workspaces/acme/kb/product-docs/search?q=installation",
		"/workspaces/acme/kb/product-docs/indexes",
		"/workspaces/acme/kb/product-docs/settings",
	}

	for _, target := range routes {
		t.Run(target, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, target, nil)
			router.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body = %q", recorder.Code, recorder.Body.String())
			}
			if recorder.Body.String() != "<html>console</html>" {
				t.Fatalf("body = %q, want embedded SPA", recorder.Body.String())
			}
			if !strings.HasPrefix(recorder.Header().Get("Content-Type"), "text/html") {
				t.Fatalf("content-type = %q, want text/html", recorder.Header().Get("Content-Type"))
			}
		})
	}
}

func TestEmbeddedSPAKeepsProtocolNamespaces(t *testing.T) {
	bundle := fstest.MapFS{
		"index.html": {Data: []byte("<html>console</html>")},
	}
	router := NewRouter(Dependencies{SPA: bundle})

	tests := []struct {
		name        string
		target      string
		wantJSON404 bool
	}{
		{name: "REST API", target: "/api/v1/unknown", wantJSON404: true},
		{name: "MCP", target: "/mcp"},
		{name: "MCP child", target: "/mcp/unknown"},
		{name: "legacy health", target: "/healthz"},
		{name: "legacy auth", target: "/auth/me"},
		{name: "legacy admin", target: "/admin/users/id/password-reset"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.target, nil))

			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404; body = %q", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "<html>console</html>") {
				t.Fatalf("protocol path %q fell through to SPA", tt.target)
			}
			isJSON := strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json")
			if isJSON != tt.wantJSON404 {
				t.Fatalf("content-type = %q, want JSON = %t", rec.Header().Get("Content-Type"), tt.wantJSON404)
			}
		})
	}
}
