import { apiClient } from '@/lib/api/client'
import {
  chunkRevisionListSchema,
  chunkRevisionSchema,
  chunkSchema,
  documentChunkPageSchema,
} from './schemas'
import type { CreateChunkRevisionInput, DocumentChunkFilters } from './types'

function chunksPath(workspaceSlug: string, kbId: string) {
  return `/workspaces/${encodeURIComponent(workspaceSlug)}/knowledge-bases/${encodeURIComponent(kbId)}`
}

export async function listDocumentChunks(
  workspaceSlug: string,
  kbId: string,
  documentId: string,
  filters: DocumentChunkFilters = {}
) {
  const response = await apiClient.get<unknown>(
    `${chunksPath(workspaceSlug, kbId)}/documents/${encodeURIComponent(documentId)}/chunks`,
    { params: filters }
  )
  return documentChunkPageSchema.parse(response.data)
}

export async function getChunk(
  workspaceSlug: string,
  kbId: string,
  chunkId: string
) {
  const response = await apiClient.get<unknown>(
    `${chunksPath(workspaceSlug, kbId)}/chunks/${encodeURIComponent(chunkId)}`
  )
  return chunkSchema.parse(response.data)
}

export async function listChunkRevisions(
  workspaceSlug: string,
  kbId: string,
  chunkId: string
) {
  const response = await apiClient.get<unknown>(
    `${chunksPath(workspaceSlug, kbId)}/chunks/${encodeURIComponent(chunkId)}/revisions`
  )
  return chunkRevisionListSchema.parse(response.data)
}

export async function createChunkRevision(
  workspaceSlug: string,
  kbId: string,
  chunkId: string,
  input: CreateChunkRevisionInput
) {
  const response = await apiClient.post<unknown>(
    `${chunksPath(workspaceSlug, kbId)}/chunks/${encodeURIComponent(chunkId)}/revisions`,
    input
  )
  return chunkRevisionSchema.parse(response.data)
}
