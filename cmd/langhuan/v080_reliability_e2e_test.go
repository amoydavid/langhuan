//go:build integration

package main

import (
	"net/http"
	"testing"
)

// TestV080RetryReindexPermissionE2E 验证 retry/reindex 端点的权限矩阵：
// member 调用必须返回 403，admin/owner 才能执行。这是 v0.8.0 安全评审的关键回归。
//
// 覆盖端点：
//   - POST /workspaces/:ws/knowledge-bases/:kb/documents/:doc/retry （需 admin/owner）
//   - POST /workspaces/:ws/jobs/:job/retry （需 admin/owner）
//   - POST /workspaces/:ws/knowledge-bases/:kb/reindex （需 admin/owner）
func TestV080RetryReindexPermissionE2E(t *testing.T) {
	env := startV030E2E(t)
	env.registerAndLogin()
	env.createWorkspaceAndKnowledgeBase()
	kbID := env.workspace.Metadata["kb_id"].(string)

	// 用 owner 上传一个文档，拿到 document_id 和 job_id。
	created := env.upload("v080-permission.md", "text/markdown",
		[]byte("# 权限矩阵\n\nretry 与 reindex 仅 admin/owner 可调。"), http.StatusCreated)
	docID := created.Document.ID.String()
	jobID := created.Job.ID.String()
	env.waitReady(created.Document.ID)

	// 邀请一个 member。
	member := registerV050Member(t, env)

	// member 调 retry document → 403。
	doJSON(t, member, env.server.URL, http.MethodPost,
		"/api/v1/workspaces/"+env.workspace.Slug+"/documents/"+docID+"/retry",
		nil, http.StatusForbidden, nil)

	// member 调 retry job → 403。
	doJSON(t, member, env.server.URL, http.MethodPost,
		"/api/v1/workspaces/"+env.workspace.Slug+"/jobs/"+jobID+"/retry",
		nil, http.StatusForbidden, nil)

	// member 调 reindex → 403。
	doJSON(t, member, env.server.URL, http.MethodPost,
		"/api/v1/workspaces/"+env.workspace.Slug+"/knowledge-bases/"+kbID+"/reindex",
		nil, http.StatusForbidden, nil)
}

// TestV080ReindexAdminSucceedsE2E 验证 admin/owner 调 reindex 返回 202（非 member）。
// 这是权限修复后的正向回归：owner 能成功触发 reindex。
func TestV080ReindexAdminSucceedsE2E(t *testing.T) {
	env := startV030E2E(t)
	env.registerAndLogin()
	env.createWorkspaceAndKnowledgeBase()
	kbID := env.workspace.Metadata["kb_id"].(string)

	// owner（首位注册者即 platform admin + workspace owner）调 reindex → 202。
	var result struct {
		GenerationID string `json:"generation_id"`
	}
	doJSON(t, env.client, env.server.URL, http.MethodPost,
		"/api/v1/workspaces/"+env.workspace.Slug+"/knowledge-bases/"+kbID+"/reindex",
		nil, http.StatusAccepted, &result)
	// reindex 创建新 building Generation。
	if result.GenerationID == "" {
		t.Fatal("reindex 返回的 generation_id 为空")
	}
}

// TestV080RetryNonFailedConflictE2E 验证 owner 对非 failed 文档调 retry 返回 409。
// 已 completed 的文档不可重试（ErrNotRetryable），防止误操作。
func TestV080RetryNonFailedConflictE2E(t *testing.T) {
	env := startV030E2E(t)
	env.registerAndLogin()
	env.createWorkspaceAndKnowledgeBase()

	created := env.upload("v080-conflict.md", "text/markdown",
		[]byte("# 非失败文档\n\ncompleted 状态不可重试。"), http.StatusCreated)
	docID := created.Document.ID.String()
	env.waitReady(created.Document.ID)

	// owner 对 completed 文档调 retry → 409 not_retryable。
	doJSON(t, env.client, env.server.URL, http.MethodPost,
		"/api/v1/workspaces/"+env.workspace.Slug+"/documents/"+docID+"/retry",
		nil, http.StatusConflict, nil)
}
