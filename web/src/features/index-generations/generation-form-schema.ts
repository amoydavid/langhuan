import { z } from 'zod'
import i18n from '@/lib/i18n'
import type { CreateIndexGenerationInput, IndexGeneration } from './types'

export const generationFormSchema = z
  .object({
    embedding_model_id: z.uuid({
      error: () =>
        i18n.t(
          'indexGenerations.generationForm.validation.selectEmbeddingModel'
        ),
    }),
    strategy: z.enum(['auto', 'heading', 'heuristic', 'recursive']),
    enable_parent_child: z.boolean(),
    parent_chunk_size: z.number().int().min(512).max(8192),
    child_chunk_size: z.number().int().min(64).max(2048),
    chunk_size: z
      .number()
      .int()
      .positive({
        error: () =>
          i18n.t(
            'indexGenerations.generationForm.validation.chunkSizePositive'
          ),
      }),
    chunk_overlap: z
      .number()
      .int()
      .min(0, {
        error: () =>
          i18n.t(
            'indexGenerations.generationForm.validation.chunkOverlapNonNegative'
          ),
      }),
    fts_config: z
      .string()
      .trim()
      .min(1, {
        error: () =>
          i18n.t('indexGenerations.generationForm.validation.selectFtsConfig'),
      }),
    vector_top_k: z.number().int().min(1).max(200),
    keyword_top_k: z.number().int().min(1).max(200),
    final_top_k: z.number().int().min(1).max(50),
    rrf_k: z.number().int().min(1),
  })
  .superRefine((values, context) => {
    const maximum = values.enable_parent_child
      ? values.parent_chunk_size
      : values.chunk_size
    if (values.chunk_overlap >= maximum) {
      context.addIssue({
        code: 'custom',
        path: ['chunk_overlap'],
        message: i18n.t(
          'indexGenerations.generationForm.validation.chunkOverlapLessThanSize'
        ),
      })
    }
    if (
      values.enable_parent_child &&
      values.child_chunk_size > values.parent_chunk_size
    ) {
      context.addIssue({
        code: 'custom',
        path: ['child_chunk_size'],
        message: i18n.t(
          'indexGenerations.generationForm.validation.childLargerThanParent'
        ),
      })
    }
  })

export type GenerationFormValues = z.infer<typeof generationFormSchema>

function configNumber(
  config: Record<string, unknown>,
  key: string,
  fallback: number
) {
  return typeof config[key] === 'number' ? config[key] : fallback
}

function configString(
  config: Record<string, unknown>,
  key: string,
  fallback: string
) {
  return typeof config[key] === 'string' ? config[key] : fallback
}

export function generationFormDefaults(
  baseGeneration: IndexGeneration
): GenerationFormValues {
  return {
    embedding_model_id: baseGeneration.embedding_model_id,
    strategy: configString(
      baseGeneration.chunking_config,
      'strategy',
      'auto'
    ) as GenerationFormValues['strategy'],
    enable_parent_child:
      typeof baseGeneration.chunking_config.enable_parent_child === 'boolean'
        ? baseGeneration.chunking_config.enable_parent_child
        : true,
    parent_chunk_size: configNumber(
      baseGeneration.chunking_config,
      'parent_chunk_size',
      4096
    ),
    child_chunk_size: configNumber(
      baseGeneration.chunking_config,
      'child_chunk_size',
      384
    ),
    chunk_size: configNumber(
      baseGeneration.chunking_config,
      'chunk_size',
      1000
    ),
    chunk_overlap: configNumber(
      baseGeneration.chunking_config,
      'chunk_overlap',
      100
    ),
    fts_config: configString(
      baseGeneration.retrieval_config,
      'fts_config',
      'zhparser'
    ),
    vector_top_k: configNumber(
      baseGeneration.retrieval_config,
      'vector_top_k',
      20
    ),
    keyword_top_k: configNumber(
      baseGeneration.retrieval_config,
      'keyword_top_k',
      20
    ),
    final_top_k: configNumber(
      baseGeneration.retrieval_config,
      'final_top_k',
      8
    ),
    rrf_k: configNumber(baseGeneration.retrieval_config, 'rrf_k', 60),
  }
}

export function toCreateGenerationInput(
  values: GenerationFormValues
): CreateIndexGenerationInput {
  return {
    embedding_model_id: values.embedding_model_id,
    chunking_config: {
      strategy: values.strategy,
      enable_parent_child: values.enable_parent_child,
      parent_chunk_size: values.parent_chunk_size,
      child_chunk_size: values.child_chunk_size,
      chunk_size: values.chunk_size,
      chunk_overlap: values.chunk_overlap,
    },
    retrieval_config: {
      fts_config: values.fts_config,
      vector_top_k: values.vector_top_k,
      keyword_top_k: values.keyword_top_k,
      final_top_k: values.final_top_k,
      rrf_k: values.rrf_k,
    },
  }
}
