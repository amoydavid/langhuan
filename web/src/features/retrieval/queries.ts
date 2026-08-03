import { queryOptions } from '@tanstack/react-query'
import { testRetrieval } from './api'
import type { RetrievalRequest } from './types'

export function retrievalTestQueryKey(
  workspaceSlug: string,
  kbId: string,
  request: RetrievalRequest
) {
  return ['retrieval-test', workspaceSlug, kbId, request] as const
}

export function retrievalTestQueryOptions(
  workspaceSlug: string,
  kbId: string,
  request: RetrievalRequest
) {
  return queryOptions({
    queryKey: retrievalTestQueryKey(workspaceSlug, kbId, request),
    queryFn: () => testRetrieval(workspaceSlug, kbId, request),
    enabled: request.query.trim().length > 0,
    staleTime: 0,
  })
}
