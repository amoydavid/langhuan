import { apiClient } from '@/lib/api/client'
import type { APIKeyCreateInput } from './schemas'
import type {
  APIKeyCreatedEnvelope,
  APIKeyDetailEnvelope,
  APIKeyListEnvelope,
  APIKeySecretEnvelope,
} from './types'

function apiKeysPath(workspaceSlug: string) {
  return `/workspaces/${encodeURIComponent(workspaceSlug)}/api-keys`
}

export async function listAPIKeys(workspaceSlug: string) {
  const response = await apiClient.get<APIKeyListEnvelope>(
    apiKeysPath(workspaceSlug)
  )
  return response.data
}

export async function getAPIKey(workspaceSlug: string, apiKeyId: string) {
  const response = await apiClient.get<APIKeyDetailEnvelope>(
    `${apiKeysPath(workspaceSlug)}/${encodeURIComponent(apiKeyId)}`
  )
  return response.data
}

export async function createAPIKey(
  workspaceSlug: string,
  input: APIKeyCreateInput
) {
  const response = await apiClient.post<APIKeyCreatedEnvelope>(
    apiKeysPath(workspaceSlug),
    input
  )
  return response.data
}

// Reveal 接口返回一次性明文；调用方负责用组件本地 state 持有，绝不进缓存。
export async function revealAPIKey(workspaceSlug: string, apiKeyId: string) {
  const response = await apiClient.post<APIKeySecretEnvelope>(
    `${apiKeysPath(workspaceSlug)}/${encodeURIComponent(apiKeyId)}/reveal`
  )
  return response.data
}

// 吊销是幂等的，成功返回 204 无 body。
export async function revokeAPIKey(workspaceSlug: string, apiKeyId: string) {
  await apiClient.delete(
    `${apiKeysPath(workspaceSlug)}/${encodeURIComponent(apiKeyId)}`
  )
}
