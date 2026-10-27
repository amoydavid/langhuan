import type { z } from 'zod'
import type {
  insightsActivityEventSchema,
  insightsActivityResponseSchema,
  insightsDocumentKindSchema,
  insightsOverviewSchema,
  queryLogSettingsResponseSchema,
  sleepingDocumentSchema,
  sleepingDocumentsResponseSchema,
  zeroRecallQueriesResponseSchema,
  zeroRecallQuerySchema,
} from './schemas'

export type InsightsDocumentKind = z.infer<typeof insightsDocumentKindSchema>
export type InsightsOverview = z.infer<typeof insightsOverviewSchema>
export type ZeroRecallQuery = z.infer<typeof zeroRecallQuerySchema>
export type ZeroRecallQueriesResponse = z.infer<
  typeof zeroRecallQueriesResponseSchema
>
export type SleepingDocument = z.infer<typeof sleepingDocumentSchema>
export type SleepingDocumentsResponse = z.infer<
  typeof sleepingDocumentsResponseSchema
>
export type InsightsActivityEvent = z.infer<typeof insightsActivityEventSchema>
export type InsightsActivityResponse = z.infer<
  typeof insightsActivityResponseSchema
>
export type QueryLogSettings = z.infer<typeof queryLogSettingsResponseSchema>

/** 健康页窗口取值（URL 参数 window），与后端 window_days 合法值对齐。 */
export const INSIGHTS_WINDOW_DAYS = [7, 14, 30] as const
export type InsightsWindowDays = (typeof INSIGHTS_WINDOW_DAYS)[number]
export const DEFAULT_INSIGHTS_WINDOW_DAYS = 14
