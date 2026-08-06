//go:build integration

package main

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// TestJinshuManagementAPIContractE2E covers the real HTTP + temporary
// PostgreSQL/Redis + worker path used by jinshu's management integration.
func TestJinshuManagementAPIContractE2E(t *testing.T) {
	env := startV030E2E(t)
	env.registerAndLogin()
	env.createWorkspaceAndKnowledgeBase()
	kbID, err := uuid.Parse(env.workspace.Metadata["kb_id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	var key struct {
		APIKey string `json:"api_key"`
	}
	env.jsonRequest(http.MethodPost, "/api/v1/workspaces/"+env.workspace.Slug+"/api-keys", map[string]any{
		"name": "jinshu management", "knowledge_base_ids": []uuid.UUID{kbID},
		"scopes":     []string{"knowledge_bases:read", "knowledge_bases:write", "documents:read", "documents:write"},
		"expiration": map[string]any{"type": "never"},
	}, http.StatusCreated, &key)
	if key.APIKey == "" {
		t.Fatal("API key is empty")
	}
	path := "/api/v1/workspaces/" + env.workspace.Slug
	bearerREST(t, env, key.APIKey, http.MethodGet, path+"/knowledge-bases", nil, http.StatusOK, nil)
	bearerREST(t, env, key.APIKey, http.MethodGet, path+"/knowledge-bases/"+kbID.String()+"/documents?kind=file", nil, http.StatusOK, nil)
	bearerREST(t, env, key.APIKey, http.MethodPost, path+"/knowledge-bases/"+kbID.String()+"/documents/text", map[string]any{
		"title": "在线排障", "content": "# 登录失败\n\n请检查令牌。", "content_type": "markdown",
	}, http.StatusCreated, nil)
	bearerREST(t, env, key.APIKey, http.MethodPost, path+"/knowledge-bases/"+kbID.String()+"/documents/text", map[string]any{
		"title": "错误", "content": "<h1>HTML</h1>", "content_type": "html",
	}, http.StatusBadRequest, nil)
	// Bearer model access is deliberately constrained to platform embeddings.
	bearerREST(t, env, key.APIKey, http.MethodGet, path+"/models?type=embedding&status=active&scope=platform", nil, http.StatusOK, nil)
	bearerREST(t, env, key.APIKey, http.MethodGet, path+"/models?type=rerank&status=active&scope=platform", nil, http.StatusBadRequest, nil)
}
