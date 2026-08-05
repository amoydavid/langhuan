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
      content: z.string(),
      source_anchor: z.record(z.string(), z.unknown()),
      score: z.number(),
      vector_score: z.number().optional(),
      keyword_score: z.number().optional(),
    })
  ),
})

export const retrievalResultsSchema = z.array(retrievalResultSchema)
