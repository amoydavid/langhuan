import { apiClient } from '@/lib/api/client'
import { workspaceSearchSettingsSchema } from './schemas'
import type { UpdateWorkspaceSearchSettingsInput } from './types'

function settingsPath(workspaceSlug: string) {
  return `/workspaces/${encodeURIComponent(workspaceSlug)}/search-settings`
}

export async function getWorkspaceSearchSettings(workspaceSlug: string) {
  const response = await apiClient.get<unknown>(settingsPath(workspaceSlug))
  return workspaceSearchSettingsSchema.parse(response.data)
}

export async function updateWorkspaceSearchSettings(
  workspaceSlug: string,
  input: UpdateWorkspaceSearchSettingsInput
) {
  const response = await apiClient.put<unknown>(
    settingsPath(workspaceSlug),
    input
  )
  return workspaceSearchSettingsSchema.parse(response.data)
}
