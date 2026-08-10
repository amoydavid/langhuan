import { apiClient } from '@/lib/api/client'
import { indexGenerationListSchema, indexGenerationSchema } from './schemas'
import type {
  ActivateIndexGenerationInput,
  CreateIndexGenerationInput,
} from './types'

function indexGenerationsPath(workspaceSlug: string, kbId: string) {
  return `/workspaces/${encodeURIComponent(workspaceSlug)}/knowledge-bases/${encodeURIComponent(kbId)}/index-generations`
}

export async function listIndexGenerations(
  workspaceSlug: string,
  kbId: string
) {
  const response = await apiClient.get<unknown>(
    indexGenerationsPath(workspaceSlug, kbId)
  )
  return indexGenerationListSchema.parse(response.data)
}

export async function createIndexGeneration(
  workspaceSlug: string,
  kbId: string,
  input: CreateIndexGenerationInput
) {
  const response = await apiClient.post<unknown>(
    indexGenerationsPath(workspaceSlug, kbId),
    input
  )
  return indexGenerationSchema.parse(response.data)
}

export async function activateIndexGeneration(
  workspaceSlug: string,
  kbId: string,
  generationId: string,
  input: ActivateIndexGenerationInput
) {
  const response = await apiClient.post<unknown>(
    `${indexGenerationsPath(workspaceSlug, kbId)}/${encodeURIComponent(generationId)}/activate`,
    input
  )
  return indexGenerationSchema.parse(response.data)
}

export interface ReindexResult {
  generation_id: string
}

export async function reindexKnowledgeBase(
  workspaceSlug: string,
  kbId: string
) {
  const response = await apiClient.post<ReindexResult>(
    `${workspacePath(workspaceSlug)}/knowledge-bases/${encodeURIComponent(kbId)}/reindex`
  )
  return response.data
}

function workspacePath(workspaceSlug: string) {
  return `/workspaces/${encodeURIComponent(workspaceSlug)}`
}
