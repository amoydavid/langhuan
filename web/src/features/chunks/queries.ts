import { queryOptions } from '@tanstack/react-query'
import { getChunk, listChunkRevisions, listDocumentChunks } from './api'
import type { DocumentChunkFilters } from './types'

export function documentChunksQueryOptions(
  workspaceSlug: string,
  kbId: string,
  documentId: string,
  activeGenerationId: string,
  filters: DocumentChunkFilters = {}
) {
  return queryOptions({
    queryKey: [
      'document-chunks',
      workspaceSlug,
      kbId,
      documentId,
      activeGenerationId,
      filters,
    ],
    queryFn: () => listDocumentChunks(workspaceSlug, kbId, documentId, filters),
    enabled: activeGenerationId.length > 0,
    staleTime: 0,
  })
}

export function chunkQueryOptions(
  workspaceSlug: string,
  kbId: string,
  chunkId: string
) {
  return queryOptions({
    queryKey: ['chunk', workspaceSlug, kbId, chunkId],
    queryFn: () => getChunk(workspaceSlug, kbId, chunkId),
    staleTime: 0,
  })
}

export function chunkRevisionsQueryOptions(
  workspaceSlug: string,
  kbId: string,
  chunkId: string
) {
  return queryOptions({
    queryKey: ['chunk-revisions', workspaceSlug, kbId, chunkId],
    queryFn: () => listChunkRevisions(workspaceSlug, kbId, chunkId),
    staleTime: 0,
  })
}
