import { z } from 'zod'

export const readinessActionSchema = z.enum([
  'configure_provider',
  'create_embedding_model',
  'create_knowledge_base',
  'add_content',
  'wait_for_processing',
  'resolve_failed_document',
  'test_retrieval',
  'none',
])

export const workspaceReadinessSchema = z.object({
  has_active_provider: z.boolean(),
  has_selectable_embedding_model: z.boolean(),
  knowledge_base_count: z.number().int().nonnegative(),
  document_counts: z.object({
    total: z.number().int().nonnegative(),
    ready: z.number().int().nonnegative(),
    processing: z.number().int().nonnegative(),
    failed: z.number().int().nonnegative(),
  }),
  searchable_knowledge_base_count: z.number().int().nonnegative(),
  recommended_action: readinessActionSchema,
  recommended_knowledge_base_id: z.uuid().nullable(),
  recommended_knowledge_base_name: z.string(),
  recommended_document_id: z.uuid().nullable(),
  recommended_document_name: z.string(),
})
