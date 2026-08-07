package http

import (
	"net/http"
	"testing"

	"github.com/dajee/langhuan/internal/domain/value"
)

func TestJinshuManagementRouteMatrix(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleAdmin, false)
	routes := map[string]bool{}
	for _, route := range f.router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	expected := []string{
		http.MethodGet + " /api/v1/workspaces/:workspace_slug/knowledge-bases",
		http.MethodGet + " /api/v1/workspaces/:workspace_slug/knowledge-bases/:id",
		http.MethodPatch + " /api/v1/workspaces/:workspace_slug/knowledge-bases/:id",
		http.MethodGet + " /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/summary",
		http.MethodGet + " /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/jobs",
		http.MethodGet + " /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/documents",
		http.MethodPost + " /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/documents/text",
		http.MethodPost + " /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/documents/faq",
		http.MethodGet + " /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/documents/:document_id/faq",
		http.MethodPut + " /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/documents/:document_id/faq",
		http.MethodGet + " /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/file-tree",
		http.MethodPost + " /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/file-tree/folders",
		http.MethodPatch + " /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/file-tree/nodes/:node_id",
		http.MethodDelete + " /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/file-tree/nodes/:node_id",
		http.MethodGet + " /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/documents/:document_id/chunks",
		http.MethodGet + " /api/v1/workspaces/:workspace_slug/models",
		// Bearer-qualified job status (documents:read; unbound -> 404).
		http.MethodGet + " /api/v1/workspaces/:workspace_slug/jobs/:id",
		// Bearer-only API Key self-introspection (scopes; no key value).
		http.MethodGet + " /api/v1/workspaces/:workspace_slug/api-key/self",
	}
	for _, route := range expected {
		if !routes[route] {
			t.Errorf("missing route %s", route)
		}
	}
	if routes[http.MethodGet+" /api/v1/workspaces/:workspace_slug/documents/:document_id/faq"] || routes[http.MethodPut+" /api/v1/workspaces/:workspace_slug/documents/:document_id/faq"] {
		t.Fatal("legacy FAQ route must not be registered")
	}
}
