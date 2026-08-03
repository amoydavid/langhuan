import { z } from 'zod'
import i18n from '@/lib/i18n'

export const knowledgeBaseSchema = z
  .object({
    name: z
      .string()
      .trim()
      .min(1, { error: () => i18n.t('knowledgeBases.schemas.nameRequired') }),
    description: z.string().trim(),
    embedding_model_id: z.uuid({
      error: () => i18n.t('knowledgeBases.schemas.embeddingModelRequired'),
    }),
    chunk_size: z
      .number({
        error: () => i18n.t('knowledgeBases.schemas.chunkSizeInvalid'),
      })
      .int({ error: () => i18n.t('knowledgeBases.schemas.chunkSizeInteger') })
      .positive({
        error: () => i18n.t('knowledgeBases.schemas.chunkSizePositive'),
      }),
    chunk_overlap: z
      .number({
        error: () => i18n.t('knowledgeBases.schemas.chunkOverlapInvalid'),
      })
      .int({
        error: () => i18n.t('knowledgeBases.schemas.chunkOverlapInteger'),
      })
      .min(0, {
        error: () => i18n.t('knowledgeBases.schemas.chunkOverlapMin'),
      }),
  })
  .refine((values) => values.chunk_overlap < values.chunk_size, {
    path: ['chunk_overlap'],
    error: () => i18n.t('knowledgeBases.schemas.chunkOverlapLessThanSize'),
  })

export type KnowledgeBaseFormValues = z.infer<typeof knowledgeBaseSchema>

const embeddingDimensionSchema = z.union([
  z.literal(798),
  z.literal(1024),
  z.literal(2048),
  z.literal(3584),
])

export const chunkingConfigResponseSchema = z.object({
  chunk_size: z.number().int().positive(),
  chunk_overlap: z.number().int().nonnegative(),
})

export const retrievalConfigResponseSchema = z.object({
  fts_config: z.string().min(1),
  vector_top_k: z.number().int().positive(),
  keyword_top_k: z.number().int().positive(),
  final_top_k: z.number().int().min(1).max(50),
  rrf_k: z.number().int().positive(),
})

export const knowledgeBaseResponseSchema = z.object({
  id: z.uuid(),
  workspace_id: z.uuid(),
  name: z.string(),
  description: z.string(),
  embedding_model_id: z.uuid(),
  embedding_model: z.object({
    id: z.uuid(),
    name: z.string(),
    display_name: z.string(),
    provider: z.string(),
    provider_display_name: z.string(),
    dimensions: embeddingDimensionSchema,
    available: z.boolean(),
  }),
  chunking_config: chunkingConfigResponseSchema,
  retrieval_config: retrievalConfigResponseSchema,
  content_version: z.number().int().nonnegative(),
  active_index_generation_id: z.uuid(),
  file_tree_root_id: z.uuid(),
  metadata: z.record(z.string(), z.unknown()),
  created_at: z.string(),
  updated_at: z.string(),
})

export const knowledgeBaseListResponseSchema = z.array(
  knowledgeBaseResponseSchema
)
