import { apiClient } from '@/lib/api/client'
import {
  insightsActivityResponseSchema,
  insightsOverviewSchema,
  queryLogSettingsResponseSchema,
  sleepingDocumentsResponseSchema,
  zeroRecallQueriesResponseSchema,
} from './schemas'
import type {
  InsightsActivityResponse,
  InsightsOverview,
  QueryLogSettings,
  SleepingDocumentsResponse,
  ZeroRecallQueriesResponse,
} from './types'

export const INSIGHTS_ZERO_RECALL_LIMIT = 10
export const INSIGHTS_SLEEPING_LIMIT = 10
export const INSIGHTS_ACTIVITY_LIMIT = 20

function insightsPath(workspaceSlug: string, kbId: string) {
  return `/workspaces/${encodeURIComponent(workspaceSlug)}/knowledge-bases/${encodeURIComponent(kbId)}/insights`
}

export async function fetchInsightsOverview(
  workspaceSlug: string,
  kbId: string,
  windowDays: number
): Promise<InsightsOverview> {
  const response = await apiClient.get<InsightsOverview>(
    `${insightsPath(workspaceSlug, kbId)}/overview`,
    { params: { window_days: windowDays } }
  )
  return insightsOverviewSchema.parse(response.data)
}

export async function fetchZeroRecallQueries(
  workspaceSlug: string,
  kbId: string,
  windowDays: number,
  limit = INSIGHTS_ZERO_RECALL_LIMIT
): Promise<ZeroRecallQueriesResponse> {
  const response = await apiClient.get<ZeroRecallQueriesResponse>(
    `${insightsPath(workspaceSlug, kbId)}/queries`,
    { params: { window_days: windowDays, limit } }
  )
  return zeroRecallQueriesResponseSchema.parse(response.data)
}

export async function fetchSleepingDocuments(
  workspaceSlug: string,
  kbId: string,
  limit = INSIGHTS_SLEEPING_LIMIT
): Promise<SleepingDocumentsResponse> {
  const response = await apiClient.get<SleepingDocumentsResponse>(
    `${insightsPath(workspaceSlug, kbId)}/documents`,
    { params: { limit } }
  )
  return sleepingDocumentsResponseSchema.parse(response.data)
}

export async function fetchActivity(
  workspaceSlug: string,
  kbId: string,
  limit = INSIGHTS_ACTIVITY_LIMIT
): Promise<InsightsActivityResponse> {
  const response = await apiClient.get<InsightsActivityResponse>(
    `${insightsPath(workspaceSlug, kbId)}/activity`,
    { params: { limit } }
  )
  return insightsActivityResponseSchema.parse(response.data)
}

export async function updateQueryLogSettings(
  workspaceSlug: string,
  kbId: string,
  enabled: boolean
): Promise<QueryLogSettings> {
  const response = await apiClient.put<QueryLogSettings>(
    `${insightsPath(workspaceSlug, kbId)}/query-log-settings`,
    { enabled }
  )
  return queryLogSettingsResponseSchema.parse(response.data)
}
