import type { z } from 'zod'
import type {
  webSourceDeletePolicySchema,
  webSourceFetchEngineSchema,
  webSourceModeSchema,
  webSourceSchema,
  webSourceStatusSchema,
  webSourceViewSchema,
} from './schemas'

export type WebSourceMode = z.infer<typeof webSourceModeSchema>
export type WebSourceStatus = z.infer<typeof webSourceStatusSchema>
export type WebSourceFetchEngine = z.infer<typeof webSourceFetchEngineSchema>
export type WebSourceDeletePolicy = z.infer<typeof webSourceDeletePolicySchema>

/** WebSource 是 kb_sources 行的 API 表示（config 为 jsonb 原始 map）。 */
export type WebSource = z.infer<typeof webSourceSchema>

/** WebSourceView 是源列表/详情响应：源投影 + 页面计数。 */
export type WebSourceView = z.infer<typeof webSourceViewSchema>

/** CreateWebSourceResult 是创建/PATCH 响应：job_id 可省略或为 null（入队 best-effort）。 */
export type WebSourceMutationResult = {
  source: WebSource
  job_id?: string | null
}

export type SyncWebSourceResult = {
  job_id?: string | null
}
