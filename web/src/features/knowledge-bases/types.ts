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

export type KnowledgeBaseSourceType = 'upload' | 'feishu_drive' | 'feishu_wiki'

export type KnowledgeBase = z.infer<typeof knowledgeBaseResponseSchema>

export type CreateKnowledgeBaseInput = {
  name: string
  description: string
  embedding_model_id: string
  chunking_config: ChunkingConfig
  source_type?: KnowledgeBaseSourceType
  source_config?: {
    root_token?: string
    root_kind?: string
    url?: string
    cron?: string
  }
  source_connection_id?: string
}
