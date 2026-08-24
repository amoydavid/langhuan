package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"time"
)

// langhuanClient 是琅嬛 REST API 的最小评测客户端：注册首用户（自动
// platform_admin）后用 HttpOnly session cookie 完成全部引导与检索。
type langhuanClient struct {
	baseURL string
	http    *http.Client
}

func newLanghuanClient(baseURL string) (*langhuanClient, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &langhuanClient{
		baseURL: baseURL,
		http:    &http.Client{Jar: jar, Timeout: 120 * time.Second},
	}, nil
}

func (c *langhuanClient) do(method, path string, body any, output any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 64*1024*1024))
	if err != nil {
		return err
	}
	if response.StatusCode >= 300 {
		return fmt.Errorf("%s %s -> HTTP %d: %s", method, path, response.StatusCode, truncateForLog(string(raw), 400))
	}
	if output != nil {
		if err := json.Unmarshal(raw, output); err != nil {
			return fmt.Errorf("解码 %s 响应失败: %w", path, err)
		}
	}
	return nil
}

func truncateForLog(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

// bootstrap 完成「注册 -> workspace -> embedding provider/model」引导，
// 返回 workspace slug 与 embedding model id。
func (c *langhuanClient) bootstrap(config evalConfig) (bootstrapResult, error) {
	var result bootstrapResult
	if err := c.do(http.MethodPost, "/api/v1/auth/register", map[string]any{
		"email": config.Server.Email, "nickname": config.Server.Nickname, "password": config.Server.Password,
	}, nil); err != nil {
		return result, fmt.Errorf("注册首用户失败（已存在的实例请用 remote 模式指向干净环境）: %w", err)
	}
	if err := c.do(http.MethodPost, "/api/v1/auth/login", map[string]any{
		"email": config.Server.Email, "password": config.Server.Password,
	}, nil); err != nil {
		return result, fmt.Errorf("登录失败: %w", err)
	}
	var workspace struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
	}
	if err := c.do(http.MethodPost, "/api/v1/workspaces", map[string]any{
		"name": "eval", "slug": "eval",
	}, &workspace); err != nil {
		return result, fmt.Errorf("创建 workspace 失败: %w", err)
	}
	result.WorkspaceSlug = workspace.Slug

	// provider/model 走平台级路由：平台作用域的端点使用受信 HTTP client，
	// 允许评测常用的本地/内网 OpenAI-compatible 端点（workspace 级自定义
	// 端点受 SSRF 策略限制，仅接受公网 HTTPS）。
	providerID, err := c.createModelProvider(config.Embedding.Provider,
		config.Embedding.ProviderConfig, config.Embedding.Credentials)
	if err != nil {
		return result, fmt.Errorf("创建 embedding provider 失败: %w", err)
	}
	modelID, err := c.createModel(providerID, "embedding", config.Embedding.ModelName,
		config.Embedding.Dimensions, config.Embedding.Parameters)
	if err != nil {
		return result, fmt.Errorf("创建 embedding model 失败: %w", err)
	}
	result.EmbeddingModelID = modelID

	if config.Rerank != nil && config.Rerank.Enabled {
		rerankProviderID, err := c.createModelProvider(config.Rerank.Provider,
			config.Rerank.ProviderConfig, config.Rerank.Credentials)
		if err != nil {
			return result, fmt.Errorf("创建 rerank provider 失败: %w", err)
		}
		rerankModelID, err := c.createModel(rerankProviderID, "rerank", config.Rerank.ModelName,
			0, config.Rerank.Parameters)
		if err != nil {
			return result, fmt.Errorf("创建 rerank model 失败: %w", err)
		}
		result.RerankModelID = rerankModelID
	}
	return result, nil
}

type bootstrapResult struct {
	WorkspaceSlug    string
	EmbeddingModelID string
	RerankModelID    string
}

func (c *langhuanClient) createModelProvider(provider string, providerConfig, credentials map[string]any) (string, error) {
	var created struct {
		ID string `json:"id"`
	}
	body := map[string]any{
		"name": "eval-" + provider, "display_name": "Eval " + provider, "description": "langhuan-eval",
		"provider": provider,
	}
	if providerConfig != nil {
		body["config"] = providerConfig
	}
	if credentials != nil {
		body["credentials"] = credentials
	}
	if err := c.do(http.MethodPost, "/api/v1/admin/model-providers", body, &created); err != nil {
		return "", err
	}
	return created.ID, nil
}

func (c *langhuanClient) createModel(providerID, modelType, modelName string, dimensions int, parameters map[string]any) (string, error) {
	var created struct {
		ID string `json:"id"`
	}
	body := map[string]any{
		"name": "eval-" + modelName, "display_name": "Eval " + modelName, "description": "langhuan-eval",
		"type": modelType, "model_name": modelName,
	}
	if dimensions > 0 {
		body["dimensions"] = dimensions
	}
	if parameters != nil {
		body["parameters"] = parameters
	}
	if err := c.do(http.MethodPost, "/api/v1/admin/model-providers/"+providerID+"/models", body, &created); err != nil {
		return "", err
	}
	return created.ID, nil
}

func (c *langhuanClient) createKnowledgeBase(slug, name, embeddingModelID string) (string, error) {
	var created struct {
		ID string `json:"id"`
	}
	if err := c.do(http.MethodPost, "/api/v1/workspaces/"+slug+"/knowledge-bases", map[string]any{
		"name": name, "description": "langhuan-eval track", "embedding_model_id": embeddingModelID,
	}, &created); err != nil {
		return "", err
	}
	return created.ID, nil
}

func (c *langhuanClient) ingestText(slug, kbID, title, content string) (string, error) {
	var created struct {
		Document struct {
			ID string `json:"id"`
		} `json:"document"`
	}
	if err := c.do(http.MethodPost, "/api/v1/workspaces/"+slug+"/knowledge-bases/"+kbID+"/documents/text", map[string]any{
		"title": title, "content": content, "content_type": "markdown",
	}, &created); err != nil {
		return "", err
	}
	return created.Document.ID, nil
}

func (c *langhuanClient) waitDocumentReady(slug, documentID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var document struct {
			Status       string `json:"status"`
			ErrorMessage string `json:"error_message"`
		}
		if err := c.do(http.MethodGet, "/api/v1/workspaces/"+slug+"/documents/"+documentID, nil, &document); err != nil {
			return err
		}
		switch document.Status {
		case "ready":
			return nil
		case "failed":
			return fmt.Errorf("文档 %s 处理失败: %s", documentID, document.ErrorMessage)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("文档 %s 在 %s 内未进入 ready", documentID, timeout)
}

// searchResponse 对应单库搜索的数组合同（v0.9.0 起运行元数据走响应头）。
type searchResultItem struct {
	ChunkID         string  `json:"chunk_id"`
	DocumentID      string  `json:"document_id"`
	Content         string  `json:"content"`
	DocumentName    string  `json:"document_name"`
	Score           float64 `json:"score"`
	MatchedChildren []struct {
		ChunkID string `json:"chunk_id"`
		Content string `json:"content"`
	} `json:"matched_children"`
}

func (c *langhuanClient) search(slug, kbID, query string, vectorTopK, keywordTopK, finalTopK int) ([]searchResultItem, error) {
	var results []searchResultItem
	body := map[string]any{
		"query":        query,
		"vector_top_k": vectorTopK, "keyword_top_k": keywordTopK, "final_top_k": finalTopK,
	}
	if err := c.do(http.MethodPost, "/api/v1/workspaces/"+slug+"/knowledge-bases/"+kbID+"/search", body, &results); err != nil {
		return nil, err
	}
	return results, nil
}

func (c *langhuanClient) setRerankSettings(slug string, enabled bool, modelID string, candidateTopK int) error {
	if modelID == "" {
		return nil
	}
	body := map[string]any{
		"rerank": map[string]any{
			"enabled": enabled, "model_id": modelID, "candidate_top_k": candidateTopK, "failure_mode": "fallback",
		},
	}
	return c.do(http.MethodPut, "/api/v1/workspaces/"+slug+"/search-settings", body, nil)
}
