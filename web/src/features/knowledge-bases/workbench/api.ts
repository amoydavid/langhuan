import { z } from 'zod'
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

const syncKnowledgeBaseResponseSchema = z.object({
  job_id: z.uuid(),
})

// 触发手动同步，返回后端受理的 job_id（HTTP 202）。
export async function syncKnowledgeBase(workspaceSlug: string, kbId: string) {
  const response = await apiClient.post<unknown>(
    `${knowledgeBasePath(workspaceSlug, kbId)}/sync`
  )
  return syncKnowledgeBaseResponseSchema.parse(response.data)
}
