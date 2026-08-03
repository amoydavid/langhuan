import { apiClient } from '@/lib/api/client'
import type { CreateWorkspaceInput, Workspace } from './types'

export async function createWorkspace(input: CreateWorkspaceInput) {
  const response = await apiClient.post<Workspace>('/workspaces', input)
  return response.data
}

export async function getWorkspace(slug: string) {
  const response = await apiClient.get<Workspace>(
    `/workspaces/${encodeURIComponent(slug)}`
  )
  return response.data
}
