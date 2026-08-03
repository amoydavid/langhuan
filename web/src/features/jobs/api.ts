import { apiClient } from '@/lib/api/client'
import { jobSummaryPageSchema } from './schemas'
import type { JobListFilters } from './types'

export async function listKnowledgeBaseJobs(
  workspaceSlug: string,
  kbId: string,
  filters: JobListFilters = {}
) {
  const response = await apiClient.get<unknown>(
    `/workspaces/${encodeURIComponent(workspaceSlug)}/knowledge-bases/${encodeURIComponent(kbId)}/jobs`,
    { params: filters }
  )
  return jobSummaryPageSchema.parse(response.data)
}
