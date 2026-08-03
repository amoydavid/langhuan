import type { z } from 'zod'
import type { DocumentKind, DocumentStatus } from '@/features/documents/types'
import type {
  indexGenerationStatusSchema,
  knowledgeBaseGenerationSummarySchema,
  knowledgeBaseSummarySchema,
  knowledgeBaseSyncStateSchema,
} from './schemas'

export type IndexGenerationStatus = z.infer<typeof indexGenerationStatusSchema>
export type KnowledgeBaseSyncState = z.infer<
  typeof knowledgeBaseSyncStateSchema
>
export type KnowledgeBaseGenerationSummary = z.infer<
  typeof knowledgeBaseGenerationSummarySchema
>
export type KnowledgeBaseSummary = z.infer<typeof knowledgeBaseSummarySchema>

export type ContentFilters = {
  kind?: DocumentKind | 'all'
  status?: DocumentStatus
  query?: string
}

export type UpdateKnowledgeBaseBasicsInput = {
  name?: string
  description?: string
}
