import { apiClient } from '@/lib/api/client'
import { workspaceReadinessSchema } from './schemas'

export async function getWorkspaceReadiness(workspaceSlug: string) {
  const response = await apiClient.get<unknown>(
    `/workspaces/${encodeURIComponent(workspaceSlug)}/readiness`
  )
  return workspaceReadinessSchema.parse(response.data)
}
