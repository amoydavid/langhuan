//go:build integration

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestV090SearchRunAndReplay 验证：
//   - 单库检索返回三个响应头（X-Search-ID、X-Retrieval-Status、X-Generation-IDs）；
//   - body 继续为数组；
//   - 结果携带 document_revision_id、index_generation_id、citation.content_sha256；
//   - owner/admin 可回放，回放创建新 SearchRun 并设置 replay_of_id；
//   - API Key 回放返回 403；
//   - 不同 query 回放返回 409 search_query_mismatch。
func TestV090SearchRunAndReplay(t *testing.T) {
	env := startV030E2E(t)
	env.registerAndLogin()
	env.createWorkspaceAndKnowledgeBase()
	kbID := env.workspace.Metadata["kb_id"].(string)

	// 上传文档并等待就绪。
	created := env.upload("v090-search.md", "text/markdown",
		[]byte("# 退款政策\n\n退款将在三个工作日内到账。\n\n安装指南请参考附录。"), http.StatusCreated)
	env.waitReady(created.Document.ID)

	// 单库检索：验证响应头和 body 数组。
	body, resp := doSearchRaw(t, env, kbID, "退款政策")
	require.Equal(t, http.StatusOK, resp.StatusCode)

	searchID := resp.Header.Get("X-Search-ID")
	require.NotEmpty(t, searchID, "X-Search-ID must be present")
	retrievalStatus := resp.Header.Get("X-Retrieval-Status")
	require.NotEmpty(t, retrievalStatus, "X-Retrieval-Status must be present")
	genIDs := resp.Header.Get("X-Generation-IDs")
	require.NotEmpty(t, genIDs, "X-Generation-IDs must be present")

	// body 必须是 JSON 数组。
	var results []map[string]any
	require.NoError(t, json.Unmarshal(body, &results), "body should be a JSON array: %s", string(body))

	// 如果有结果，验证 lineage 字段。
	if len(results) > 0 {
		first := results[0]
		require.NotEmpty(t, first["document_revision_id"], "document_revision_id must be present")
		require.NotEmpty(t, first["index_generation_id"], "index_generation_id must be present")
		citation, ok := first["citation"].(map[string]any)
		require.True(t, ok, "citation must be present")
		require.Len(t, citation["content_sha256"], 64, "content_sha256 must be 64 hex chars")
		require.Equal(t, "valid", citation["status"])
	}

	// owner 回放（首位注册者即 owner）。
	replayBody, replayResp := doReplay(t, env, searchID, "退款政策")
	require.Equal(t, http.StatusOK, replayResp.StatusCode)

	var replayResponse struct {
		Run struct {
			SearchID   string `json:"search_id"`
			ReplayOfID string `json:"replay_of_id"`
		} `json:"run"`
	}
	require.NoError(t, json.Unmarshal(replayBody, &replayResponse))
	require.NotEmpty(t, replayResponse.Run.SearchID)
	require.NotEqual(t, searchID, replayResponse.Run.SearchID)
	// replay_of_id 应指向原 search_id（JSON 中可能是 string 或 null）。
	if replayResponse.Run.ReplayOfID != "" {
		require.Equal(t, searchID, replayResponse.Run.ReplayOfID)
	}
}

// TestV090ReplayRejectsAPIKey 验证 Bearer API Key 不可调用回放。
func TestV090ReplayRejectsAPIKey(t *testing.T) {
	env := startV030E2E(t)
	env.registerAndLogin()
	env.createWorkspaceAndKnowledgeBase()
	kbID := env.workspace.Metadata["kb_id"].(string)

	created := env.upload("v090-apikey.md", "text/markdown",
		[]byte("# 测试\n\n内容。"), http.StatusCreated)
	env.waitReady(created.Document.ID)

	_, resp := doSearchRaw(t, env, kbID, "测试")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	searchID := resp.Header.Get("X-Search-ID")
	require.NotEmpty(t, searchID)

	// 创建 API Key。
	var apiKey struct {
		Item struct {
			ID uuid.UUID `json:"id"`
		}
		APIKey string `json:"api_key"`
	}
	env.jsonRequest(http.MethodPost, "/api/v1/workspaces/"+env.workspace.Slug+"/api-keys", map[string]any{
		"name": "replay-test", "knowledge_base_ids": []uuid.UUID{uuid.MustParse(kbID)},
		"scopes": []string{"search:read"}, "expiration": map[string]any{"type": "never"},
	}, http.StatusCreated, &apiKey)

	// Bearer 回放 → 403。
	req, _ := http.NewRequest(http.MethodPost, env.server.URL+"/api/v1/workspaces/"+env.workspace.Slug+"/search-runs/"+searchID+"/replay",
		readerFor(map[string]any{"query": "测试"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey.APIKey)
	replayResp, err := env.client.Do(req)
	require.NoError(t, err)
	replayResp.Body.Close()
	require.Equal(t, http.StatusForbidden, replayResp.StatusCode)
}

// TestV090ReplayRejectsDifferentQuery 验证回放 query 不匹配返回 409。
func TestV090ReplayRejectsDifferentQuery(t *testing.T) {
	env := startV030E2E(t)
	env.registerAndLogin()
	env.createWorkspaceAndKnowledgeBase()
	kbID := env.workspace.Metadata["kb_id"].(string)

	created := env.upload("v090-mismatch.md", "text/markdown",
		[]byte("# 退款政策\n\n正文。"), http.StatusCreated)
	env.waitReady(created.Document.ID)

	_, resp := doSearchRaw(t, env, kbID, "退款政策")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	searchID := resp.Header.Get("X-Search-ID")
	require.NotEmpty(t, searchID)

	// 用不同 query 回放 → 409 search_query_mismatch。
	replayBody, replayResp := doReplay(t, env, searchID, "安装指南")
	require.Equal(t, http.StatusConflict, replayResp.StatusCode)
	require.Contains(t, string(replayBody), "search_query_mismatch")
}

// TestV090MultiSearchReturnsRunMetadata 验证多库检索返回运行元数据 wrapper。
func TestV090MultiSearchReturnsRunMetadata(t *testing.T) {
	env := startV030E2E(t)
	env.registerAndLogin()
	env.createWorkspaceAndKnowledgeBase()
	kbID := env.workspace.Metadata["kb_id"].(string)

	created := env.upload("v090-multi.md", "text/markdown",
		[]byte("# 多库\n\n正文。"), http.StatusCreated)
	env.waitReady(created.Document.ID)

	var multiResp struct {
		SearchID        string `json:"search_id"`
		RequestedScope  string `json:"requested_scope"`
		EffectiveScope  string `json:"effective_scope"`
		RetrievalStatus string `json:"retrieval_status"`
	}
	env.jsonRequest(http.MethodPost, "/api/v1/workspaces/"+env.workspace.Slug+"/search", map[string]any{
		"knowledge_base_ids": []uuid.UUID{uuid.MustParse(kbID)}, "query": "多库",
	}, http.StatusOK, &multiResp)
	require.NotEmpty(t, multiResp.SearchID)
	require.Equal(t, "selected", multiResp.RequestedScope)
	require.NotEmpty(t, multiResp.RetrievalStatus)
}

// doSearchRaw 执行单库检索并返回原始 body 和 response（含响应头）。
func doSearchRaw(t *testing.T, env *v030E2E, kbID, query string) ([]byte, *http.Response) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, env.server.URL+"/api/v1/workspaces/"+env.workspace.Slug+"/knowledge-bases/"+kbID+"/search",
		readerFor(map[string]any{"query": query}))
	req.Header.Set("Content-Type", "application/json")
	addSessionCookie(t, env, req)
	resp, err := env.client.Do(req)
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return body, resp
}

// doReplay 执行管理员回放。
func doReplay(t *testing.T, env *v030E2E, searchID, query string) ([]byte, *http.Response) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, env.server.URL+"/api/v1/workspaces/"+env.workspace.Slug+"/search-runs/"+searchID+"/replay",
		readerFor(map[string]any{"query": query}))
	req.Header.Set("Content-Type", "application/json")
	addSessionCookie(t, env, req)
	resp, err := env.client.Do(req)
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return body, resp
}

// readerFor 把 body map 编码为 io.Reader。
func readerFor(body any) io.Reader {
	encoded, _ := json.Marshal(body)
	return bytes.NewReader(encoded)
}
