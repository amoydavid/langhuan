import { z } from 'zod'

// 与后端 insights 组端点（v1.4.0 spec §7）一一对应：
// GET .../insights/{overview,queries,documents,activity} + PUT .../insights/query-log-settings。

export const insightsDocumentKindSchema = z.enum(['faq', 'web', 'file'])

export const insightsActivityKindSchema = z.enum(['sync', 'revision', 'delete'])

export const insightsOverviewSchema = z.object({
  window_days: z.number().int().positive(),
  runs_total: z.number().int().nonnegative(),
  runs_available: z.number().int().nonnegative(),
  runs_empty: z.number().int().nonnegative(),
  runs_degraded: z.number().int().nonnegative(),
  runs_failed_excluded: z.number().int().nonnegative(),
  // 空召回率 = empty / (available + empty + degraded)，failed 不计入分母。
  empty_recall_rate: z.number().min(0).max(1),
  // has_previous_window=false 时后端不保证回填上期值，nullish 兜底。
  previous_empty_recall_rate: z.number().min(0).max(1).nullish(),
  has_previous_window: z.boolean(),
  sleeping_documents_count: z.number().int().nonnegative(),
  sleeping_insufficient_sample: z.boolean(),
  recent_changes_count: z.number().int().nonnegative(),
  query_log_enabled: z.boolean(),
})

// 零召回查询组（按 query_hash 聚合）；latest_query_text 仅在该 KB
// query_log_enabled=true 时出现（D1 明文门控，前端不得假设必有）。
export const zeroRecallQuerySchema = z.object({
  query_hash: z.string().min(1),
  query_chars: z.number().int().positive(),
  hits: z.number().int().positive(),
  first_seen_at: z.string(),
  last_seen_at: z.string(),
  latest_query_text: z.string().optional(),
})

export const zeroRecallQueriesResponseSchema = z.object({
  query_log_enabled: z.boolean(),
  window_days: z.number().int().positive(),
  queries: z.array(zeroRecallQuerySchema),
})

// 沉睡内容：窗口起点前已索引、窗口内零引用、未删除的文档（anti-join citations）。
export const sleepingDocumentSchema = z.object({
  document_id: z.uuid(),
  kind: insightsDocumentKindSchema,
  title: z.string(),
  indexed_at: z.string(),
})

export const sleepingDocumentsResponseSchema = z.object({
  insufficient_sample: z.boolean(),
  documents: z.array(sleepingDocumentSchema),
})

// 内容动态（§4.4 三源 UNION）：sync 对应源同步，revision 对应 document_revisions，
// delete 对应文档软删；可选字段按事件类型出现。
export const insightsActivityEventSchema = z.object({
  kind: insightsActivityKindSchema,
  target_document_id: z.uuid().nullish(),
  source_id: z.uuid().nullish(),
  source_name: z.string().nullish(),
  document_kind: insightsDocumentKindSchema.nullish(),
  document_title: z.string().nullish(),
  revision_reason: z.string().nullish(),
  success: z.boolean(),
  error_message: z.string().nullish(),
  occurred_at: z.string(),
})

export const insightsActivityResponseSchema = z.object({
  events: z.array(insightsActivityEventSchema),
})

export const queryLogSettingsResponseSchema = z.object({
  enabled: z.boolean(),
  retention_days: z.number().int().positive(),
})

// 健康页窗口选项：URL 参数 window（7|14|30），默认 14 在读取侧兜底；
// 字符串形态（链接/手输）做 coerce，非法值 catch 回 undefined（默认窗口）。
const windowDaysSchema = z
  .union([z.number(), z.string().transform((value) => Number(value))])
  .pipe(z.union([z.literal(7), z.literal(14), z.literal(30)]))

export const insightsSearchSchema = z.object({
  window: windowDaysSchema.optional().catch(undefined),
})
