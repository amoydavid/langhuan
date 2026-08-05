import type { z } from 'zod'
import type { EmbeddingDimension } from '@/features/models/types'
import type { knowledgeBaseResponseSchema } from './schemas'

export type ChunkingConfig = {
  strategy: 'auto' | 'heading' | 'heuristic' | 'recursive'
  enable_parent_child: boolean
  parent_chunk_size: number
  child_chunk_size: number
  chunk_size: number
  chunk_overlap: number
}

export type EmbeddingModelSummary = {
  id: string
  name: string
  display_name: string
  provider: string
  provider_display_name: string
  dimensions: EmbeddingDimension
  available: boolean
}

export type KnowledgeBase = z.infer<typeof knowledgeBaseResponseSchema>

export type CreateKnowledgeBaseInput = {
  name: string
  description: string
  embedding_model_id: string
  chunking_config: ChunkingConfig
}
