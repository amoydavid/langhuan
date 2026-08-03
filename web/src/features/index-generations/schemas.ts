import { z } from 'zod'
import { indexGenerationStatusSchema } from '@/features/knowledge-bases/workbench/schemas'

export const manualEditDispositionSchema = z.enum([
  'not_applicable',
  'pending',
  'archive_confirmed',
])

export const indexGenerationSchema = z.object({
  id: z.uuid(),
  workspace_id: z.uuid(),
  knowledge_base_id: z.uuid(),
  base_generation_id: z.uuid().optional(),
  embedding_model_id: z.uuid(),
  provider_id: z.uuid(),
  model_name: z.string(),
  display_label: z.string().min(1),
  embedding_dimension: z.number().int().positive(),
  chunker_version: z.number().int().positive(),
  chunking_config: z.record(z.string(), z.unknown()),
  retrieval_config: z.record(z.string(), z.unknown()),
  config_hash: z.string(),
  source_content_version: z.number().int().nonnegative(),
  indexed_content_version: z.number().int().nonnegative(),
  status: indexGenerationStatusSchema,
  document_count: z.number().int().nonnegative(),
  chunk_count: z.number().int().nonnegative(),
  indexed_count: z.number().int().nonnegative(),
  manual_edit_count: z.number().int().nonnegative(),
  disabled_chunk_count: z.number().int().nonnegative(),
  manual_edit_disposition: manualEditDispositionSchema,
  error_class: z.string().optional().default(''),
  error_message: z.string().optional().default(''),
  created_at: z.string(),
  ready_at: z.string().nullable().optional(),
  activated_at: z.string().nullable().optional(),
  retired_at: z.string().nullable().optional(),
})

export const indexGenerationListSchema = z.array(indexGenerationSchema)
