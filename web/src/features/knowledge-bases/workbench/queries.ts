import { queryOptions } from '@tanstack/react-query'
import { getKnowledgeBaseSummary } from './api'
import type { ContentFilters } from './types'

export function knowledgeBaseSummaryQueryOptions(
  workspaceSlug: string,
  kbId: string
) {
  return queryOptions({
    queryKey: ['knowledge-base-summary', workspaceSlug, kbId],
    queryFn: () => getKnowledgeBaseSummary(workspaceSlug, kbId),
    staleTime: 0,
  })
}

export function contentQueryKey(
  workspaceSlug: string,
  kbId: string,
  filters: ContentFilters
) {
  return ['content', workspaceSlug, kbId, filters] as const
}

export function fileTreeQueryKey(workspaceSlug: string, kbId: string) {
  return ['file-tree', workspaceSlug, kbId] as const
}
