import { z } from 'zod'

// 枚举与后端 value.WebSourceMode / WebSourceFetchEngine / KbSourceStatus /
// SourceDeletePolicy 一一对应（v1.3.0 spec §4.1/§6.1）。
export const webSourceModeSchema = z.enum(['crawl', 'sitemap', 'urls'])

export const webSourceStatusSchema = z.enum(['active', 'disabled'])

export const webSourceFetchEngineSchema = z.enum([
  'auto',
  'http',
  'render',
  'firecrawl',
  'jina',
])

export const webSourceDeletePolicySchema = z.enum(['keep', 'remove'])

export const webSourceExtractModeSchema = z.enum(['readability', 'full'])

const jsonRecordSchema = z.record(z.string(), z.unknown())

// sync_last_result 由后端 SyncResult 写入（source_sync_store.go），本端只读展示，
// 键全部可选：源从未同步时为空对象。
export const webSourceSyncResultSchema = z.object({
  status: z.string().optional(),
  complete: z.boolean().optional(),
  synced_documents: z.number().int().nonnegative().optional(),
  skipped_documents: z.number().int().nonnegative().optional(),
  failed_documents: z.number().int().nonnegative().optional(),
  deleted_documents: z.number().int().nonnegative().optional(),
  finished_at: z.string().optional(),
})

export const webSourceSchema = z.object({
  id: z.uuid(),
  workspace_id: z.uuid(),
  knowledge_base_id: z.uuid(),
  kind: z.literal('web'),
  name: z.string().min(1),
  status: webSourceStatusSchema,
  config: jsonRecordSchema,
  sync_cursor: jsonRecordSchema,
  next_sync_at: z.string().nullable(),
  sync_requested_force: z.boolean(),
  sync_last_result: webSourceSyncResultSchema,
  created_at: z.string(),
  updated_at: z.string(),
})

export const webSourceViewSchema = z.object({
  source: webSourceSchema,
  page_count: z.number().int().nonnegative(),
  failed_page_count: z.number().int().nonnegative(),
})

export const webSourceListResponseSchema = z.array(webSourceViewSchema)

// 创建/PATCH 响应：job_id 可省略（入队 best-effort，客户端不得假设必有；
// sync 端点无 omitempty，job 为 nil 时会返回显式 null，故用 nullish）。
export const webSourceMutationResponseSchema = z.object({
  source: webSourceSchema,
  job_id: z.uuid().nullish(),
})

// 手动同步响应：202 + job_id。
export const webSourceSyncResponseSchema = z.object({
  job_id: z.uuid().nullish(),
})
