import {
  mutationOptions,
  type QueryClient,
  queryOptions,
} from '@tanstack/react-query'
import { getKnowledgeBaseSummary, syncKnowledgeBase } from './api'
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

// 手动同步：成功后刷新 KB summary，使头部同步状态及时反映新任务。
export function syncKnowledgeBaseMutationOptions(
  workspaceSlug: string,
  kbId: string,
  queryClient: QueryClient
) {
  return mutationOptions({
    mutationKey: ['knowledge-base-sync', workspaceSlug, kbId],
    mutationFn: () => syncKnowledgeBase(workspaceSlug, kbId),
    onSuccess: () =>
      queryClient.invalidateQueries({
        queryKey: ['knowledge-base-summary', workspaceSlug, kbId],
      }),
  })
}
