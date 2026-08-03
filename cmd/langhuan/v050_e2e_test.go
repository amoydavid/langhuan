//go:build integration

package main

import (
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/domain/value"
	webspa "github.com/dajee/langhuan/web"
)

func TestV050WebEmbedCanonicalRoutesE2E(t *testing.T) {
	if webspa.SPA == nil {
		t.Skip("requires web_embed build tag")
	}
	env := startV030E2E(t)
	routes := []string{
		"/workspaces/acme/kb/product-docs",
		"/workspaces/acme/kb/product-docs/content/all",
		"/workspaces/acme/kb/product-docs/content/files",
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
		response, err := env.client.Get(env.server.URL + target)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if response.StatusCode != http.StatusOK ||
			!strings.HasPrefix(response.Header.Get("Content-Type"), "text/html") ||
			!strings.Contains(string(body), `<div id="root">`) {
			t.Fatalf("GET %s status/type/body = %d %q %q", target, response.StatusCode, response.Header.Get("Content-Type"), body)
		}
	}

	apiResponse := doJSON(t, env.client, env.server.URL, http.MethodGet, "/api/v1/v050-unknown", nil, http.StatusNotFound, nil)
	if !strings.HasPrefix(apiResponse.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("API content type = %q", apiResponse.Header.Get("Content-Type"))
	}
	mcpResponse, err := env.client.Get(env.server.URL + "/mcp/unknown")
	if err != nil {
		t.Fatal(err)
	}
	_ = mcpResponse.Body.Close()
	if strings.HasPrefix(mcpResponse.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("MCP namespace returned SPA content type %q", mcpResponse.Header.Get("Content-Type"))
	}
}

func TestV050MemberContentAndIndexPermissionE2E(t *testing.T) {
	env := startV030E2E(t)
	env.registerAndLogin()
	env.createWorkspaceAndKnowledgeBase()
	kbID := env.workspace.Metadata["kb_id"].(string)

	member := registerV050Member(t, env)

	var tree dto.FileTree
	doJSON(t, member, env.server.URL, http.MethodGet,
		"/api/v1/workspaces/"+env.workspace.Slug+"/knowledge-bases/"+kbID+"/file-tree",
		nil, http.StatusOK, &tree)
	if tree.Root == nil || tree.Root.Name == "" {
		t.Fatalf("file tree root = %#v, want readable root", tree.Root)
	}
	var folder dto.FileTreeNode
	doJSON(t, member, env.server.URL, http.MethodPost,
		"/api/v1/workspaces/"+env.workspace.Slug+"/knowledge-bases/"+kbID+"/file-tree/folders",
		map[string]any{"parent_id": tree.Root.ID, "name": "成员资料"}, http.StatusCreated, &folder)
	if folder.Name != "成员资料" {
		t.Fatalf("folder name = %q", folder.Name)
	}

	created := env.uploadWithClient(member, "member-guide.md", "text/markdown", []byte("# 成员指南\n\ninstallation handbook for Langhuan retrieval."), http.StatusCreated)
	ready := env.waitReady(created.Document.ID)
	if ready.Title != "member-guide.md" {
		t.Fatalf("document title = %q", ready.Title)
	}

	var faq dto.FAQDocument
	doJSON(t, member, env.server.URL, http.MethodPost,
		"/api/v1/workspaces/"+env.workspace.Slug+"/knowledge-bases/"+kbID+"/documents/faq",
		map[string]any{
			"title": "退款政策", "questions": []string{"how to refund?", "when is refund settled?"},
			"answer": "请在订单页提交退款申请。",
		}, http.StatusCreated, &faq)
	if faq.Document == nil || faq.Document.Title != "退款政策" || len(faq.Questions) != 2 {
		t.Fatalf("FAQ = %#v", faq)
	}
	env.waitReady(faq.Document.ID)

	var results []*dto.SearchResult
	doJSON(t, member, env.server.URL, http.MethodPost,
		"/api/v1/workspaces/"+env.workspace.Slug+"/knowledge-bases/"+kbID+"/search",
		map[string]any{"query": "installation", "final_top_k": 8}, http.StatusOK, &results)
	if len(results) == 0 || results[0].DocumentName == "" {
		t.Fatalf("search results = %#v, want readable source", results)
	}

	results = nil
	doJSON(t, member, env.server.URL, http.MethodPost,
		"/api/v1/workspaces/"+env.workspace.Slug+"/knowledge-bases/"+kbID+"/search",
		map[string]any{"query": "refund", "final_top_k": 8}, http.StatusOK, &results)
	faqFound := false
	for _, result := range results {
		if result.DocumentKind == value.DocumentKindFAQ && result.DocumentName == "退款政策" {
			faqFound = true
			break
		}
	}
	if !faqFound {
		t.Fatalf("FAQ search results = %#v, want readable FAQ source", results)
	}

	doJSON(t, member, env.server.URL, http.MethodPost,
		"/api/v1/workspaces/"+env.workspace.Slug+"/knowledge-bases/"+kbID+"/chunks/"+uuid.NewString()+"/revisions",
		map[string]any{
			"base_revision_id": uuid.New(), "content": "成员不能修改", "context_header": "安装", "enabled": true,
		}, http.StatusForbidden, nil)
	doJSON(t, member, env.server.URL, http.MethodPost,
		"/api/v1/workspaces/"+env.workspace.Slug+"/knowledge-bases/"+kbID+"/index-generations",
		map[string]any{}, http.StatusForbidden, nil)
}

func registerV050Member(t *testing.T, env *v030E2E) *http.Client {
	t.Helper()
	email := "v050-member-" + uuid.NewString() + "@example.com"
	var invitation struct {
		InviteURL string `json:"invite_url"`
	}
	doJSON(t, env.client, env.server.URL, http.MethodPost,
		"/api/v1/workspaces/"+env.workspace.Slug+"/invitations",
		map[string]any{"invited_email": email, "role": value.RoleMember},
		http.StatusCreated, &invitation)
	inviteURL, err := url.Parse(invitation.InviteURL)
	if err != nil {
		t.Fatal(err)
	}
	token := path.Base(inviteURL.Path)
	if token == "." || token == "/" || token == "invitations" {
		t.Fatalf("invalid invite URL %q", invitation.InviteURL)
	}

	client := newCookieClient(t)
	doJSON(t, client, env.server.URL, http.MethodPost, "/api/v1/auth/register",
		map[string]any{
			"email": email, "nickname": "内容成员", "password": "Passw0rd!", "invitation_token": token,
		}, http.StatusCreated, &dto.AuthenticatedUser{})
	return client
}
