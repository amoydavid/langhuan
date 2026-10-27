import { z } from 'zod'
import { documentKindSchema } from '@/features/documents/schemas'

export const retrievalRequestSchema = z.object({
  query: z.string().trim().min(1),
  vector_top_k: z.number().int().positive().optional(),
  keyword_top_k: z.number().int().positive().optional(),
  final_top_k: z.number().int().min(1).max(50).optional(),
})

export const rankingStageSchema = z.enum(['rrf', 'rerank', 'rrf_fallback'])

export const retrievalResultSchema = z.object({
  chunk_id: z.uuid(),
  chunk_revision_id: z.uuid(),
  document_id: z.uuid(),
  document_kind: documentKindSchema,
  content: z.string(),
  document_name: z.string().min(1),
  source_anchor: z.record(z.string(), z.unknown()),
  score: z.number(),
  vector_score: z.number().optional(),
  keyword_score: z.number().optional(),
  rerank_score: z.number().optional(),
  ranking_stage: rankingStageSchema,
  metadata: z.record(z.string(), z.unknown()),
  matched_children: z.array(
    z.object({
      chunk_id: z.uuid(),
      chunk_revision_id: z.uuid(),
      role: z.enum(['child', 'flat']),
      source_anchor: z.record(z.string(), z.unknown()),
      score: z.number(),
      vector_score: z.number().optional(),
      keyword_score: z.number().optional(),
    })
  ),
})

export const retrievalResultsSchema = z.array(retrievalResultSchema)

/**
 * 结果 metadata 中的新鲜度字段（v1.4.0 §9.3.8）：后端仅在存在时返回
 * `document_updated_at`（RFC3339）；web 文档附 `source_last_fetch_at`。
 * metadata 整体是 record，渲染前用本 schema 对可选字段做一次安全解析。
 */
export const resultFreshnessSchema = z.object({
  document_updated_at: z.string().optional(),
  source_last_fetch_at: z.string().optional(),
})
