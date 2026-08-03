// Workspace API Key 的对外类型，严格对齐后端 dto.WorkspaceAPIKey。
// status 使用后端派生状态字符串：active | expiring | expired | revoked。

export type APIKeyStatus = 'active' | 'expiring' | 'expired' | 'revoked'

export type APIKeyScope =
  | 'knowledge_bases:write'
  | 'documents:read'
  | 'documents:write'
  | 'search:read'

export type APIKeyKnowledgeBaseSummary = {
  id: string
  name: string
}

export type APIKeyActorSummary = {
  id: string
  nickname: string
}

export type APIKey = {
  id: string
  name: string
  token_prefix: string
  knowledge_bases: APIKeyKnowledgeBaseSummary[]
  scopes: APIKeyScope[]
  status: APIKeyStatus
  expires_at: string | null
  last_used_at: string | null
  revoked_at: string | null
  created_by: APIKeyActorSummary | null
  created_at: string
}

// 列表响应：{ items, base_url, rest_base_url, mcp_url }
export type APIKeyListEnvelope = {
  items: APIKey[]
  base_url: string
  rest_base_url: string
  mcp_url: string
}

// 详情响应：{ item, base_url, rest_base_url, mcp_url }
export type APIKeyDetailEnvelope = {
  item: APIKey
  base_url: string
  rest_base_url: string
  mcp_url: string
}

// 创建响应：{ api_key(一次性明文), item, base_url, rest_base_url, mcp_url }
export type APIKeyCreatedEnvelope = {
  api_key: string
  item: APIKey
  base_url: string
  rest_base_url: string
  mcp_url: string
}

// Reveal 响应：{ api_key(一次性明文), base_url, rest_base_url, mcp_url }
export type APIKeySecretEnvelope = {
  api_key: string
  base_url: string
  rest_base_url: string
  mcp_url: string
}
