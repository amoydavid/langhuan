import { apiClient } from '@/lib/api/client'
import {
  knowledgeBaseListResponseSchema,
  knowledgeBaseResponseSchema,
} from './schemas'
import type { CreateKnowledgeBaseInput, KnowledgeBase } from './types'

function knowledgeBasesPath(workspaceSlug: string) {
  return `/workspaces/${encodeURIComponent(workspaceSlug)}/knowledge-bases`
}

export async function listKnowledgeBases(workspaceSlug: string) {
  const response = await apiClient.get<KnowledgeBase[]>(
    knowledgeBasesPath(workspaceSlug)
  )
  return knowledgeBaseListResponseSchema.parse(response.data)
}

export async function createKnowledgeBase(
  workspaceSlug: string,
  input: CreateKnowledgeBaseInput
) {
  const response = await apiClient.post<KnowledgeBase>(
    knowledgeBasesPath(workspaceSlug),
    input
  )
  return knowledgeBaseResponseSchema.parse(response.data)
}

export async function getKnowledgeBase(workspaceSlug: string, kbId: string) {
  const response = await apiClient.get<KnowledgeBase>(
    `${knowledgeBasesPath(workspaceSlug)}/${encodeURIComponent(kbId)}`
  )
  return knowledgeBaseResponseSchema.parse(response.data)
}
