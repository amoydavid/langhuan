import { apiClient } from '@/lib/api/client'
import { retrievalRequestSchema, retrievalResultsSchema } from './schemas'
import type { RetrievalRequest } from './types'

/** 控制台检索测试标记（v1.4.0 §9.2）：后端仅对 Session principal 记 transport='console'。 */
export const consoleSearchHeader = {
  'X-Langhuan-Console-Search': '1',
} as const

export async function testRetrieval(
  workspaceSlug: string,
  kbId: string,
  request: RetrievalRequest
) {
  const body = retrievalRequestSchema.parse(request)
  const response = await apiClient.post<unknown>(
    `/workspaces/${encodeURIComponent(workspaceSlug)}/knowledge-bases/${encodeURIComponent(kbId)}/search`,
    body,
    { headers: consoleSearchHeader }
  )
  return retrievalResultsSchema.parse(response.data)
}
