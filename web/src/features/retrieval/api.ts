import { apiClient } from '@/lib/api/client'
import { retrievalRequestSchema, retrievalResultsSchema } from './schemas'
import type { RetrievalRequest } from './types'

export async function testRetrieval(
  workspaceSlug: string,
  kbId: string,
  request: RetrievalRequest
) {
  const body = retrievalRequestSchema.parse(request)
  const response = await apiClient.post<unknown>(
    `/workspaces/${encodeURIComponent(workspaceSlug)}/knowledge-bases/${encodeURIComponent(kbId)}/search`,
    body
  )
  return retrievalResultsSchema.parse(response.data)
}
