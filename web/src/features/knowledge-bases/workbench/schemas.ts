import { z } from 'zod'
import { jobSummarySchema } from '@/features/jobs/schemas'

export const indexGenerationStatusSchema = z.enum([
  'building',
  'ready',
  'stale',
  'failed',
  'retired',
])

export const knowledgeBaseSyncStateSchema = z.enum([
  'synced',
  'updating',
  'failed',
  'candidate_ready',
])

export const knowledgeBaseGenerationSummarySchema = z.object({
  id: z.uuid(),
  display_label: z.string().min(1),
  status: indexGenerationStatusSchema,
  model_display_name: z.string().min(1),
  embedding_dimension: z.number().int().positive(),
  chunker_version: z.number().int().positive(),
  chunking_config: z.record(z.string(), z.unknown()),
  retrieval_config: z.record(z.string(), z.unknown()),
  source_content_version: z.number().int().nonnegative(),
  indexed_content_version: z.number().int().nonnegative(),
  document_count: z.number().int().nonnegative(),
  chunk_count: z.number().int().nonnegative(),
  indexed_count: z.number().int().nonnegative(),
  manual_edit_count: z.number().int().nonnegative(),
  disabled_chunk_count: z.number().int().nonnegative(),
  error_message: z.string().optional().default(''),
  created_at: z.string(),
  ready_at: z.string().nullable().optional(),
  activated_at: z.string().nullable().optional(),
})

export const knowledgeBaseSummarySchema = z.object({
  knowledge_base_id: z.uuid(),
  knowledge_base_name: z.string().min(1),
  content_version: z.number().int().nonnegative(),
  document_counts: z.object({
    total: z.number().int().nonnegative(),
    file: z.number().int().nonnegative(),
    faq: z.number().int().nonnegative(),
    web: z.number().int().nonnegative(),
    ready: z.number().int().nonnegative(),
    processing: z.number().int().nonnegative(),
    failed: z.number().int().nonnegative(),
  }),
  active_generation: knowledgeBaseGenerationSummarySchema.nullable(),
  candidate_generation: knowledgeBaseGenerationSummarySchema.nullable(),
  sync_state: knowledgeBaseSyncStateSchema,
  recent_jobs: z.array(jobSummarySchema),
  blockers: z.array(
    z.object({
      code: z.string(),
      resource_type: z.string(),
      resource_id: z.uuid(),
      resource_display_name: z.string().min(1),
      message: z.string(),
    })
  ),
})
