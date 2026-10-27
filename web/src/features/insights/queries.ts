import type { QueryClient } from '@tanstack/react-query'
import { mutationOptions, queryOptions } from '@tanstack/react-query'
import {
  fetchActivity,
  fetchInsightsOverview,
  fetchSleepingDocuments,
  fetchZeroRecallQueries,
  INSIGHTS_ACTIVITY_LIMIT,
  INSIGHTS_SLEEPING_LIMIT,
  INSIGHTS_ZERO_RECALL_LIMIT,
  updateQueryLogSettings,
} from './api'

export function kbInsightsRootQueryKey(workspaceSlug: string, kbId: string) {
  return ['kb-insights', workspaceSlug, kbId] as const
}

export function insightsOverviewQueryOptions(
  workspaceSlug: string,
  kbId: string,
  windowDays: number
) {
  return queryOptions({
    queryKey: [
      ...kbInsightsRootQueryKey(workspaceSlug, kbId),
      'overview',
      windowDays,
    ],
    queryFn: () => fetchInsightsOverview(workspaceSlug, kbId, windowDays),
    staleTime: 5_000,
  })
}

export function zeroRecallQueriesQueryOptions(
  workspaceSlug: string,
  kbId: string,
  windowDays: number,
  limit = INSIGHTS_ZERO_RECALL_LIMIT
) {
  return queryOptions({
    queryKey: [
      ...kbInsightsRootQueryKey(workspaceSlug, kbId),
      'queries',
      windowDays,
      limit,
    ],
    queryFn: () =>
      fetchZeroRecallQueries(workspaceSlug, kbId, windowDays, limit),
    staleTime: 5_000,
  })
}

// 沉睡内容固定 30 天口径（spec §9.2），不接受窗口参数。
export function sleepingDocumentsQueryOptions(
  workspaceSlug: string,
  kbId: string,
  limit = INSIGHTS_SLEEPING_LIMIT
) {
  return queryOptions({
    queryKey: [
      ...kbInsightsRootQueryKey(workspaceSlug, kbId),
      'documents',
      limit,
    ],
    queryFn: () => fetchSleepingDocuments(workspaceSlug, kbId, limit),
    staleTime: 5_000,
  })
}

export function insightsActivityQueryOptions(
  workspaceSlug: string,
  kbId: string,
  limit = INSIGHTS_ACTIVITY_LIMIT
) {
  return queryOptions({
    queryKey: [
      ...kbInsightsRootQueryKey(workspaceSlug, kbId),
      'activity',
      limit,
    ],
    queryFn: () => fetchActivity(workspaceSlug, kbId, limit),
    staleTime: 5_000,
  })
}

export function updateQueryLogSettingsMutationOptions(
  workspaceSlug: string,
  kbId: string,
  queryClient: QueryClient
) {
  return mutationOptions({
    mutationKey: ['kb-insights-settings', workspaceSlug, kbId],
    mutationFn: (enabled: boolean) =>
      updateQueryLogSettings(workspaceSlug, kbId, enabled),
    onSuccess: () => {
      // 开关改变 overview 的 query_log_enabled 与 queries 的明文可见性，
      // 前缀失效把四个 scope 一并刷新。
      void queryClient.invalidateQueries({
        queryKey: kbInsightsRootQueryKey(workspaceSlug, kbId),
      })
    },
  })
}
