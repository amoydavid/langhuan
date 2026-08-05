import type { z } from 'zod'
import type {
  indexGenerationListSchema,
  indexGenerationSchema,
  manualEditDispositionSchema,
} from './schemas'

export type ManualEditDisposition = z.infer<typeof manualEditDispositionSchema>
export type IndexGeneration = z.infer<typeof indexGenerationSchema>
export type IndexGenerationList = z.infer<typeof indexGenerationListSchema>

export type CreateIndexGenerationInput = {
  embedding_model_id: string
  chunking_config: {
    strategy: 'auto' | 'heading' | 'heuristic' | 'recursive'
    enable_parent_child: boolean
    parent_chunk_size: number
    child_chunk_size: number
    chunk_size: number
    chunk_overlap: number
  }
  retrieval_config: {
    fts_config: string
    vector_top_k: number
    keyword_top_k: number
    final_top_k: number
    rrf_k: number
  }
  rerank?: {
    enabled: boolean
    model_id?: string
    candidate_top_k?: number
    failure_mode?: 'fallback' | 'fail'
  }
}

export type ActivateIndexGenerationInput = {
  archive_manual_edits: boolean
}
