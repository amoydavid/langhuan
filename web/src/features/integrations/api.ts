import { apiClient } from '@/lib/api/client'
import {
  createSourceConnectionSchema,
  sourceConnectionListResponseSchema,
  sourceConnectionResponseSchema,
} from './schemas'
import type {
  CreateSourceConnectionInput,
  SourceConnection,
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
