import { z } from 'zod'
import i18n from '@/lib/i18n'

export const knowledgeBaseSourceTypeSchema = z.enum([
  'upload',
  'feishu_drive',
  'feishu_wiki',
])

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
    strategy: z.enum(['auto', 'heading', 'heuristic', 'recursive']),
    enable_parent_child: z.boolean(),
    parent_chunk_size: z.number().int().min(512).max(8192),
    child_chunk_size: z.number().int().min(64).max(2048),
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
    source_type: knowledgeBaseSourceTypeSchema,
    source_connection_id: z.string().optional(),
    root_token: z.string().optional(),
    sync_enabled: z.boolean(),
    cron: z.string().optional(),
  })
  .superRefine((values, context) => {
    const maximum = values.enable_parent_child
      ? values.parent_chunk_size
      : values.chunk_size
    if (values.chunk_overlap >= maximum) {
      context.addIssue({
        code: 'custom',
        path: ['chunk_overlap'],
        message: i18n.t('knowledgeBases.schemas.chunkOverlapLessThanSize'),
      })
    }
    if (
      values.enable_parent_child &&
      values.child_chunk_size > values.parent_chunk_size
    ) {
      context.addIssue({
        code: 'custom',
        path: ['child_chunk_size'],
        message: i18n.t('knowledgeBases.schemas.childLargerThanParent'),
      })
    }
    if (values.source_type !== 'upload') {
      if (!values.source_connection_id) {
        context.addIssue({
          code: 'custom',
          path: ['source_connection_id'],
          message: i18n.t('knowledgeBases.schemas.sourceConnectionRequired'),
        })
      }
      if (!values.root_token?.trim()) {
        context.addIssue({
          code: 'custom',
          path: ['root_token'],
          message: i18n.t('knowledgeBases.schemas.rootTokenRequired'),
        })
      }
    }
  })

export type KnowledgeBaseFormValues = z.infer<typeof knowledgeBaseSchema>

const embeddingDimensionSchema = z.union([
  z.literal(798),
  z.literal(1024),
  z.literal(2048),
  z.literal(3584),
])

export const chunkingConfigResponseSchema = z.object({
  strategy: z.enum(['auto', 'heading', 'heuristic', 'recursive']),
  enable_parent_child: z.boolean(),
  parent_chunk_size: z.number().int().min(512).max(8192),
  child_chunk_size: z.number().int().min(64).max(2048),
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
  source_type: knowledgeBaseSourceTypeSchema.catch('upload'),
  source_config: z
    .object({
      root_token: z.string().optional(),
      root_kind: z.string().optional(),
      url: z.string().optional(),
      cron: z.string().optional(),
    })
    .nullish(),
  source_connection_id: z.string().nullish(),
  created_at: z.string(),
  updated_at: z.string(),
})

export const knowledgeBaseListResponseSchema = z.array(
  knowledgeBaseResponseSchema
)
