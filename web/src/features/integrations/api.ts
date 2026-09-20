import { apiClient } from '@/lib/api/client'
import {
  createSourceConnectionSchema,
  sourceConnectionListResponseSchema,
  sourceConnectionResponseSchema,
  sourceConnectionTestResultSchema,
} from './schemas'
import type {
  CreateSourceConnectionInput,
  SourceConnection,
  SourceConnectionTestResult,
  UpdateSourceConnectionInput,
} from './types'

function sourceConnectionsPath(workspaceSlug: string) {
  return `/workspaces/${encodeURIComponent(workspaceSlug)}/source-connections`
}

export async function listSourceConnections(
  workspaceSlug: string
): Promise<SourceConnection[]> {
  const response = await apiClient.get<SourceConnection[]>(
    sourceConnectionsPath(workspaceSlug)
  )
  return sourceConnectionListResponseSchema.parse(response.data)
}

export async function createSourceConnection(
  workspaceSlug: string,
  input: CreateSourceConnectionInput
): Promise<SourceConnection> {
  const payload = createSourceConnectionSchema.parse(input)
  const response = await apiClient.post<SourceConnection>(
    sourceConnectionsPath(workspaceSlug),
    payload
  )
  return sourceConnectionResponseSchema.parse(response.data)
}

export async function getSourceConnection(
  workspaceSlug: string,
  connectionId: string
): Promise<SourceConnection> {
  const response = await apiClient.get<SourceConnection>(
    `${sourceConnectionsPath(workspaceSlug)}/${encodeURIComponent(connectionId)}`
  )
  return sourceConnectionResponseSchema.parse(response.data)
}

export async function updateSourceConnection(
  workspaceSlug: string,
  connectionId: string,
  input: UpdateSourceConnectionInput
): Promise<SourceConnection> {
  const response = await apiClient.patch<SourceConnection>(
    `${sourceConnectionsPath(workspaceSlug)}/${encodeURIComponent(connectionId)}`,
    input
  )
  return sourceConnectionResponseSchema.parse(response.data)
}

export async function deleteSourceConnection(
  workspaceSlug: string,
  connectionId: string
): Promise<void> {
  await apiClient.delete(
    `${sourceConnectionsPath(workspaceSlug)}/${encodeURIComponent(connectionId)}`
  )
}

// 连接测试契约（T13）：探测成功与失败都返回 HTTP 200 + 结果体，前端按 ok 分支
// 展示；「无法测试」（不存在 404、已停用/feishu 不支持 400）才走异常路径。
export async function testSourceConnection(
  workspaceSlug: string,
  connectionId: string
): Promise<SourceConnectionTestResult> {
  const response = await apiClient.post<SourceConnectionTestResult>(
    `${sourceConnectionsPath(workspaceSlug)}/${encodeURIComponent(connectionId)}/test`
  )
  return sourceConnectionTestResultSchema.parse(response.data)
}
