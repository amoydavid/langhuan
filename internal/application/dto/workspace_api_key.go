package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/value"
)

// WorkspaceAPIKeyKnowledgeBaseSummary 是 API Key 绑定的知识库可读摘要。
type WorkspaceAPIKeyKnowledgeBaseSummary struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// WorkspaceAPIKeyActorSummary 是创建/吊销者的可读摘要，不含敏感信息。
type WorkspaceAPIKeyActorSummary struct {
	ID       uuid.UUID `json:"id"`
	Nickname string    `json:"nickname"`
}

// WorkspaceAPIKey 是对外暴露的安全 API Key 视图，绝不包含明文、hash 或密文。
type WorkspaceAPIKey struct {
	ID             uuid.UUID                             `json:"id"`
	Name           string                                `json:"name"`
	TokenPrefix    string                                `json:"token_prefix"`
	KnowledgeBases []WorkspaceAPIKeyKnowledgeBaseSummary `json:"knowledge_bases"`
	Scopes         []value.APIScope                      `json:"scopes"`
	Status         value.APIKeyStatus                    `json:"status"`
	ExpiresAt      *time.Time                            `json:"expires_at"`
	LastUsedAt     *time.Time                            `json:"last_used_at"`
	RevokedAt      *time.Time                            `json:"revoked_at"`
	CreatedBy      *WorkspaceAPIKeyActorSummary          `json:"created_by"`
	CreatedAt      time.Time                             `json:"created_at"`
}

// WorkspaceAPIKeyListEnvelope 是列表响应，附带全局公开地址。
type WorkspaceAPIKeyListEnvelope struct {
	Items       []WorkspaceAPIKey `json:"items"`
	BaseURL     string            `json:"base_url"`
	RESTBaseURL string            `json:"rest_base_url"`
	MCPURL      string            `json:"mcp_url"`
}

// WorkspaceAPIKeyDetailEnvelope 是详情响应。
type WorkspaceAPIKeyDetailEnvelope struct {
	Item        WorkspaceAPIKey `json:"item"`
	BaseURL     string          `json:"base_url"`
	RESTBaseURL string          `json:"rest_base_url"`
	MCPURL      string          `json:"mcp_url"`
}

// WorkspaceAPIKeySecretEnvelope 是创建/Reveal 响应，携带一次性明文与派生地址。
// 该响应必须以 no-store 方式返回，且不进入任何持久缓存。
type WorkspaceAPIKeySecretEnvelope struct {
	APIKey      string `json:"api_key"`
	BaseURL     string `json:"base_url"`
	RESTBaseURL string `json:"rest_base_url"`
	MCPURL      string `json:"mcp_url"`
}
