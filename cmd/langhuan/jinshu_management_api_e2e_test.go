//go:build integration

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
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
	var unboundKB dto.KnowledgeBase
	env.jsonRequest(http.MethodPost, "/api/v1/workspaces/"+env.workspace.Slug+"/knowledge-bases", map[string]any{
		"name": "unbound", "embedding_model_id": env.modelID,
	}, http.StatusCreated, &unboundKB)

	var key struct {
		APIKey string `json:"api_key"`
		Item   struct {
			ID uuid.UUID `json:"id"`
		} `json:"item"`
	}
	path := "/api/v1/workspaces/" + env.workspace.Slug
	env.jsonRequest(http.MethodPost, path+"/api-keys", map[string]any{
		"name": "jinshu management", "knowledge_base_ids": []uuid.UUID{kbID},
		"scopes":     []string{"knowledge_bases:read", "knowledge_bases:write", "documents:read", "documents:write"},
		"expiration": map[string]any{"type": "never"},
	}, http.StatusCreated, &key)
	if key.APIKey == "" || key.Item.ID == uuid.Nil {
		t.Fatalf("API key response = %#v", key)
	}

	var listed []*dto.KnowledgeBase
	bearerREST(t, env, key.APIKey, http.MethodGet, path+"/knowledge-bases", nil, http.StatusOK, &listed)
	if len(listed) != 1 || listed[0].ID != kbID {
		t.Fatalf("Bearer KB list = %#v, want only %s", listed, kbID)
	}
	for _, suffix := range []string{"", "/summary", "/documents", "/file-tree", "/jobs"} {
		bearerREST(t, env, key.APIKey, http.MethodGet, path+"/knowledge-bases/"+unboundKB.ID.String()+suffix, nil, http.StatusNotFound, nil)
	}
	bearerREST(t, env, key.APIKey, http.MethodPatch, path+"/knowledge-bases/"+kbID.String(), map[string]any{"description": "managed"}, http.StatusOK, nil)

	var readOnly struct {
		APIKey string `json:"api_key"`
	}
	env.jsonRequest(http.MethodPost, path+"/api-keys", map[string]any{
		"name": "read only", "knowledge_base_ids": []uuid.UUID{kbID}, "scopes": []string{"knowledge_bases:read"},
		"expiration": map[string]any{"type": "never"},
	}, http.StatusCreated, &readOnly)
	bearerREST(t, env, readOnly.APIKey, http.MethodPatch, path+"/knowledge-bases/"+kbID.String(), map[string]any{"description": "nope"}, http.StatusForbidden, nil)
	bearerREST(t, env, readOnly.APIKey, http.MethodGet, path+"/knowledge-bases/"+kbID.String()+"/documents", nil, http.StatusForbidden, nil)

	var textResult service.IngestDocumentResult
	bearerREST(t, env, key.APIKey, http.MethodPost, path+"/knowledge-bases/"+kbID.String()+"/documents/text", map[string]any{
		"title": "在线排障", "content": "# 登录失败\n\n请检查令牌。", "content_type": "markdown",
	}, http.StatusCreated, &textResult)
	if textResult.Document == nil || textResult.Document.ID == uuid.Nil || textResult.Job == nil || textResult.Job.ID == uuid.Nil {
		t.Fatalf("text ingest result = %#v", textResult)
	}
	bearerREST(t, env, key.APIKey, http.MethodPost, path+"/knowledge-bases/"+kbID.String()+"/documents/text", map[string]any{
		"title": "空内容", "content": "   ", "content_type": "markdown",
	}, http.StatusBadRequest, nil)
	bearerREST(t, env, key.APIKey, http.MethodPost, path+"/knowledge-bases/"+kbID.String()+"/documents/text", map[string]any{
		"title": "超限", "content": strings.Repeat("x", 8<<20+1), "content_type": "markdown",
	}, http.StatusBadRequest, nil)
	env.waitReady(textResult.Document.ID)
	var files []*dto.Document
	bearerREST(t, env, key.APIKey, http.MethodGet, path+"/knowledge-bases/"+kbID.String()+"/documents?kind=file", nil, http.StatusOK, &files)
	if len(files) == 0 || files[0].Kind != value.DocumentKindFile {
		t.Fatalf("file kind list = %#v", files)
	}
	bearerREST(t, env, key.APIKey, http.MethodGet, path+"/knowledge-bases/"+kbID.String()+"/documents?kind=invalid", nil, http.StatusBadRequest, nil)
	var chunks dto.DocumentChunkPage
	bearerREST(t, env, key.APIKey, http.MethodGet, path+"/knowledge-bases/"+kbID.String()+"/documents/"+textResult.Document.ID.String()+"/chunks?limit=50", nil, http.StatusOK, &chunks)
	if len(chunks.Items) == 0 {
		t.Fatalf("document chunks = %#v", chunks)
	}
	bearerREST(t, env, key.APIKey, http.MethodGet, path+"/knowledge-bases/"+unboundKB.ID.String()+"/documents/"+textResult.Document.ID.String()+"/chunks", nil, http.StatusNotFound, nil)
	var jobs dto.JobSummaryPage
	bearerREST(t, env, key.APIKey, http.MethodGet, path+"/knowledge-bases/"+kbID.String()+"/jobs?document_id="+textResult.Document.ID.String(), nil, http.StatusOK, &jobs)
	if len(jobs.Items) == 0 {
		t.Fatalf("job list = %#v", jobs)
	}

	var tree dto.FileTree
	bearerREST(t, env, key.APIKey, http.MethodGet, path+"/knowledge-bases/"+kbID.String()+"/file-tree", nil, http.StatusOK, &tree)
	if tree.Root == nil {
		t.Fatal("file tree root is nil")
	}
	fileNode := findTreeDocument(tree.Root, textResult.Document.ID)
	if fileNode == nil {
		t.Fatalf("file tree does not contain document %s", textResult.Document.ID)
	}
	bearerREST(t, env, key.APIKey, http.MethodPatch, path+"/knowledge-bases/"+kbID.String()+"/file-tree/nodes/"+fileNode.ID.String(), map[string]any{"name": "在线排障-重命名"}, http.StatusNoContent, nil)
	var renamed dto.Document
	env.jsonRequest(http.MethodGet, "/api/v1/workspaces/"+env.workspace.Slug+"/documents/"+textResult.Document.ID.String(), nil, http.StatusOK, &renamed)
	if renamed.Title != "在线排障-重命名" {
		t.Fatalf("renamed document title = %q", renamed.Title)
	}
	var folder dto.FileTreeNode
	bearerREST(t, env, key.APIKey, http.MethodPost, path+"/knowledge-bases/"+kbID.String()+"/file-tree/folders", map[string]any{"parent_id": tree.Root.ID, "name": "程序化目录"}, http.StatusCreated, &folder)
	bearerREST(t, env, key.APIKey, http.MethodPatch, path+"/knowledge-bases/"+kbID.String()+"/file-tree/nodes/"+folder.ID.String(), map[string]any{"name": "重命名目录"}, http.StatusNoContent, nil)
	bearerREST(t, env, key.APIKey, http.MethodDelete, path+"/knowledge-bases/"+kbID.String()+"/file-tree/nodes/"+folder.ID.String(), nil, http.StatusNoContent, nil)
	var nonEmpty dto.FileTreeNode
	bearerREST(t, env, key.APIKey, http.MethodPost, path+"/knowledge-bases/"+kbID.String()+"/file-tree/folders", map[string]any{"parent_id": tree.Root.ID, "name": "非空目录"}, http.StatusCreated, &nonEmpty)
	var child dto.FileTreeNode
	bearerREST(t, env, key.APIKey, http.MethodPost, path+"/knowledge-bases/"+kbID.String()+"/file-tree/folders", map[string]any{"parent_id": nonEmpty.ID, "name": "子目录"}, http.StatusCreated, &child)
	bearerREST(t, env, key.APIKey, http.MethodDelete, path+"/knowledge-bases/"+kbID.String()+"/file-tree/nodes/"+nonEmpty.ID.String(), nil, http.StatusConflict, nil)
	bearerREST(t, env, key.APIKey, http.MethodDelete, path+"/knowledge-bases/"+kbID.String()+"/file-tree/nodes/"+child.ID.String(), nil, http.StatusNoContent, nil)
	bearerREST(t, env, key.APIKey, http.MethodDelete, path+"/knowledge-bases/"+kbID.String()+"/file-tree/nodes/"+nonEmpty.ID.String(), nil, http.StatusNoContent, nil)

	var faq dto.FAQDocument
	bearerREST(t, env, key.APIKey, http.MethodPost, path+"/knowledge-bases/"+kbID.String()+"/documents/faq", map[string]any{"title": "退款 FAQ", "questions": []string{"如何退款？"}, "answer": "请提交申请。"}, http.StatusCreated, &faq)
	if faq.Document == nil || faq.Document.Kind != value.DocumentKindFAQ || faq.Revision == nil {
		t.Fatalf("FAQ create = %#v", faq)
	}
	faq = waitFAQ(t, env, key.APIKey, path, kbID, faq.Document.ID)
	var faqs []*dto.Document
	bearerREST(t, env, key.APIKey, http.MethodGet, path+"/knowledge-bases/"+kbID.String()+"/documents?kind=faq", nil, http.StatusOK, &faqs)
	if len(faqs) == 0 || faqs[0].Kind != value.DocumentKindFAQ {
		t.Fatalf("FAQ kind list = %#v", faqs)
	}
	bearerREST(t, env, key.APIKey, http.MethodPut, path+"/knowledge-bases/"+kbID.String()+"/documents/"+faq.Document.ID.String()+"/faq", map[string]any{
		"questions": []string{"缺少基准版本"}, "answer": "无效",
	}, http.StatusBadRequest, nil)
	bearerREST(t, env, key.APIKey, http.MethodPut, path+"/knowledge-bases/"+kbID.String()+"/documents/"+faq.Document.ID.String()+"/faq", map[string]any{
		"base_revision_id": faq.Revision.ID, "questions": []string{"退款怎么处理？"}, "answer": "请提交申请。",
	}, http.StatusAccepted, nil)
	bearerREST(t, env, key.APIKey, http.MethodGet, path+"/knowledge-bases/"+unboundKB.ID.String()+"/documents/"+faq.Document.ID.String()+"/faq", nil, http.StatusNotFound, nil)

	bearerREST(t, env, key.APIKey, http.MethodPost, path+"/knowledge-bases/"+kbID.String()+"/documents/text", map[string]any{
		"title": "错误", "content": "<h1>HTML</h1>", "content_type": "html",
	}, http.StatusBadRequest, nil)
	var models []*dto.Model
	bearerREST(t, env, key.APIKey, http.MethodGet, path+"/models?type=embedding&status=active&scope=platform", nil, http.StatusOK, &models)
	for _, model := range models {
		if model.Type != value.ModelTypeEmbedding || model.Status != value.ModelStatusActive {
			t.Fatalf("Bearer model filter leaked model = %#v", model)
		}
	}
	bearerREST(t, env, key.APIKey, http.MethodGet, path+"/models?type=rerank&status=active&scope=platform", nil, http.StatusBadRequest, nil)
	for _, query := range []string{
		"type=embedding&status=disabled&scope=platform",
		"type=embedding&status=active&scope=workspace",
		"type=all&status=active&scope=platform",
	} {
		bearerREST(t, env, key.APIKey, http.MethodGet, path+"/models?"+query, nil, http.StatusBadRequest, nil)
	}

	// An invalid Bearer never falls back to the valid Session cookie installed in
	// env.client's jar.
	req, _ := http.NewRequest(http.MethodGet, env.server.URL+path+"/knowledge-bases", nil)
	req.Header.Set("Authorization", "Bearer definitely-invalid")
	resp, err := env.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("invalid Bearer status = %d", resp.StatusCode)
	}

	env.jsonRequest(http.MethodDelete, path+"/api-keys/"+key.Item.ID.String(), nil, http.StatusNoContent, nil)
	bearerREST(t, env, key.APIKey, http.MethodGet, path+"/knowledge-bases", nil, http.StatusUnauthorized, nil)
}

func findTreeDocument(node *dto.FileTreeNode, documentID uuid.UUID) *dto.FileTreeNode {
	if node == nil {
		return nil
	}
	if node.DocumentID != nil && *node.DocumentID == documentID {
		return node
	}
	for _, child := range node.Children {
		if found := findTreeDocument(child, documentID); found != nil {
			return found
		}
	}
	return nil
}

func waitFAQ(t *testing.T, env *v030E2E, secret, path string, kbID, documentID uuid.UUID) dto.FAQDocument {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var got dto.FAQDocument
		req, _ := http.NewRequest(http.MethodGet, env.server.URL+path+"/knowledge-bases/"+kbID.String()+"/documents/"+documentID.String()+"/faq", nil)
		req.Header.Set("Authorization", "Bearer "+secret)
		resp, err := env.client.Do(req)
		if err == nil {
			data, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				if err := json.Unmarshal(data, &got); err != nil {
					t.Fatal(err)
				}
				return got
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("FAQ %s was not published", documentID)
	return dto.FAQDocument{}
}
