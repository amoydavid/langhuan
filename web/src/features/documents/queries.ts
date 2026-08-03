import { queryOptions } from '@tanstack/react-query'
import { getDocument, getJob, listDocuments } from './api'
import type { Document } from './types'

export function documentsQueryOptions(workspaceSlug: string, kbId: string) {
  return queryOptions({
    queryKey: ['documents', workspaceSlug, kbId],
    queryFn: () => listDocuments(workspaceSlug, kbId),
    staleTime: 5_000,
  })
}

export function documentQueryOptions(
  workspaceSlug: string,
  documentId: string
) {
  return queryOptions({
    queryKey: ['document', workspaceSlug, documentId],
    queryFn: () => getDocument(workspaceSlug, documentId),
    refetchInterval: (query) => {
      const data = query.state.data as Document | undefined
      return data?.status === 'pending' || data?.status === 'processing'
        ? 2_000
        : false
    },
    staleTime: 0,
  })
}

export function jobQueryOptions(workspaceSlug: string, jobId: string) {
  return queryOptions({
    queryKey: ['job', workspaceSlug, jobId],
    queryFn: () => getJob(workspaceSlug, jobId),
    staleTime: 0,
  })
}
