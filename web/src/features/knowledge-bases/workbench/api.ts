import { knowledgeBaseResponseSchema } from '@/features/knowledge-bases/schemas'
import { apiClient } from '@/lib/api/client'
import { knowledgeBaseSummarySchema } from './schemas'
import type { UpdateKnowledgeBaseBasicsInput } from './types'

function knowledgeBasePath(workspaceSlug: string, kbId: string) {
  return `/workspaces/${encodeURIComponent(workspaceSlug)}/knowledge-bases/${encodeURIComponent(kbId)}`
}

export async function getKnowledgeBaseSummary(
  workspaceSlug: string,
  kbId: string
) {
  const response = await apiClient.get<unknown>(
    `${knowledgeBasePath(workspaceSlug, kbId)}/summary`
  )
  return knowledgeBaseSummarySchema.parse(response.data)
}

export async function updateKnowledgeBaseBasics(
  workspaceSlug: string,
  kbId: string,
  input: UpdateKnowledgeBaseBasicsInput
) {
  const response = await apiClient.patch<unknown>(
    knowledgeBasePath(workspaceSlug, kbId),
    input
  )
  return knowledgeBaseResponseSchema.parse(response.data)
}
