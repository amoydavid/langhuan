//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/domain/value"
)

// TestV060WorkspaceAPIKeyRESTFlowE2E 验证 v0.6.0 的 API Key 主链路：
// admin 创建 key -> Bearer REST 多库检索 -> reveal -> 吊销后 401。
func TestV060WorkspaceAPIKeyRESTFlowE2E(t *testing.T) {
	env := startV030E2E(t)
	env.registerAndLogin()
	env.createWorkspaceAndKnowledgeBase()
	kbIDStr := env.workspace.Metadata["kb_id"].(string)
	kbID, err := uuid.Parse(kbIDStr)
	require.NoError(t, err)
	adminID := env.user.ID

	// 创建绑定该知识库、search:read scope、不限期的 API Key。
	var created struct {
		APIKey string `json:"api_key"`
		Item   struct {
			ID          uuid.UUID `json:"id"`
			TokenPrefix string    `json:"token_prefix"`
		} `json:"item"`
	}
	env.jsonRequest(http.MethodPost, "/api/v1/workspaces/"+env.workspace.Slug+"/api-keys", map[string]any{
		"name": "检索 Agent", "knowledge_base_ids": []uuid.UUID{kbID},
		"scopes": []string{"search:read", "documents:read"}, "expiration": map[string]any{"type": "never"},
	}, http.StatusCreated, &created)
	require.NotEmpty(t, created.APIKey)
	require.Contains(t, created.APIKey, "lhk_")
	secret := created.APIKey

	// 用 Bearer 进行多库检索（REST），证明 key 可用。
	var searchResp struct {
		Results []*dto.SearchResult `json:"results"`
	}
	bearerREST(t, env, secret, http.MethodPost, "/api/v1/workspaces/"+env.workspace.Slug+"/search",
		map[string]any{"knowledge_base_ids": []uuid.UUID{kbID}, "query": "installation"}, http.StatusOK, &searchResp)
	// 不强制要求结果非空（取决于是否已导入文档），但证明 key 鉴权通过。

	// reveal 返回相同明文（Session）。
	var revealed struct {
		APIKey string `json:"api_key"`
	}
	env.jsonRequest(http.MethodPost, "/api/v1/workspaces/"+env.workspace.Slug+"/api-keys/"+created.Item.ID.String()+"/reveal", nil, http.StatusOK, &revealed)
	require.Equal(t, secret, revealed.APIKey)

	// 吊销。
	env.jsonRequest(http.MethodDelete, "/api/v1/workspaces/"+env.workspace.Slug+"/api-keys/"+created.Item.ID.String(), nil, http.StatusNoContent, nil)

	// 吊销后 Bearer REST 检索应 401。
	bearerREST(t, env, secret, http.MethodPost, "/api/v1/workspaces/"+env.workspace.Slug+"/search",
		map[string]any{"knowledge_base_ids": []uuid.UUID{kbID}, "query": "installation"}, http.StatusUnauthorized, nil)

	// 吊销后仍可 reveal（Session owner/admin）。
	env.jsonRequest(http.MethodPost, "/api/v1/workspaces/"+env.workspace.Slug+"/api-keys/"+created.Item.ID.String()+"/reveal", nil, http.StatusOK, &revealed)
	require.Equal(t, secret, revealed.APIKey)

	_ = adminID
}

// TestV060MCPRejectsCookieAndRequiresBearerE2E 验证 /mcp 只接受 Bearer。
func TestV060MCPRejectsCookieAndRequiresBearerE2E(t *testing.T) {
	env := startV030E2E(t)
	env.registerAndLogin()

	// 无 Authorization（仅有效 Session cookie）应 401。
	req, _ := http.NewRequest(http.MethodPost, env.server.URL+"/mcp", bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)))
	req.Header.Set("content-type", "application/json")
	addSessionCookie(t, env, req)
	resp, err := env.client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.Equal(t, "Bearer", resp.Header.Get("WWW-Authenticate"))
}

// TestV060MemberCannotManageAPIKeysE2E 验证 member 不能管理 API Key。
func TestV060MemberCannotManageAPIKeysE2E(t *testing.T) {
	env := startV030E2E(t)
	env.registerAndLogin()
	env.createWorkspaceAndKnowledgeBase()

	// 把当前用户降为 member（owner 创建 workspace 时默认 owner；通过邀请一个 member）。
	// 简化：直接用 admin 邀请 member，member 登录后访问 api-keys 应被拒。
	memberClient := registerV050Member(t, env)
	listResp, err := memberClient.Get(env.server.URL + "/api/v1/workspaces/" + env.workspace.Slug + "/api-keys")
	require.NoError(t, err)
	listResp.Body.Close()
	// member 不满足 admin 角色 -> 403。
	if listResp.StatusCode != http.StatusForbidden && listResp.StatusCode != http.StatusNotFound {
		t.Fatalf("member api-keys status = %d, want 403/404", listResp.StatusCode)
	}
}

// bearerREST 用 Bearer API Key 发起 JSON 请求。
func bearerREST(t *testing.T, env *v030E2E, secret, method, path string, body any, wantStatus int, output any) {
	t.Helper()
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	req, err := http.NewRequest(method, env.server.URL+path, bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+secret)
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	resp, err := env.client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("bearerREST %s %s status = %d, want %d", method, path, resp.StatusCode, wantStatus)
	}
	if output != nil && resp.StatusCode < 400 {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(output))
	}
}

// addSessionCookie 把 env 的会话 cookie 加到请求（复用已登录 env）。
func addSessionCookie(t *testing.T, env *v030E2E, req *http.Request) {
	t.Helper()
	cookies := env.client.Jar.Cookies(req.URL)
	for _, c := range cookies {
		req.AddCookie(c)
	}
}

// registerV050Member 在 env 的 workspace 中创建并登录一个 member（复用 v050 helper）。
// 占位：若 v050 helper 不可用则跳过。
var _ = base64.StdEncoding
var _ = context.Background
var _ = value.RoleMember

// TestV060MCPToolsListWithValidBearerE2E 验证有效 Bearer 能通过 /mcp 完成
// tools/list，且只看到 search:read scope 允许的工具。
func TestV060MCPToolsListWithValidBearerE2E(t *testing.T) {
	env := startV030E2E(t)
	env.registerAndLogin()
	env.createWorkspaceAndKnowledgeBase()
	kbIDStr := env.workspace.Metadata["kb_id"].(string)
	kbID, err := uuid.Parse(kbIDStr)
	require.NoError(t, err)

	// 创建只含 search:read scope 的 key。
	var created struct {
		APIKey string `json:"api_key"`
	}
	env.jsonRequest(http.MethodPost, "/api/v1/workspaces/"+env.workspace.Slug+"/api-keys", map[string]any{
		"name": "只读检索", "knowledge_base_ids": []uuid.UUID{kbID},
		"scopes": []string{"search:read"}, "expiration": map[string]any{"type": "never"},
	}, http.StatusCreated, &created)

	// 通过 /mcp 发起 JSON-RPC tools/list。
	body := mcpJSONRPC(t, 1, "tools/list", map[string]any{})
	req, _ := http.NewRequest(http.MethodPost, env.server.URL+"/mcp", bytes.NewReader(body))
	req.Header.Set("content-type", "application/json")
	req.Header.Set("Authorization", "Bearer "+created.APIKey)
	resp, err := env.client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var rpcResp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rpcResp))
	names := make([]string, 0, len(rpcResp.Result.Tools))
	for _, tool := range rpcResp.Result.Tools {
		names = append(names, tool.Name)
	}
	// search:read 只应看到 knowledge_search。
	require.ElementsMatch(t, []string{"knowledge_search"}, names, "search:read key 只看到 knowledge_search")
}

// mcpJSONRPC 构造一个 JSON-RPC 请求体。
func mcpJSONRPC(t *testing.T, id int, method string, params any) []byte {
	t.Helper()
	payload := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	b, err := json.Marshal(payload)
	require.NoError(t, err)
	return b
}
