import type { z } from 'zod'
import type {
  retrievalRequestSchema,
  retrievalResultSchema,
  retrievalResultsSchema,
} from './schemas'

export type RetrievalRequest = z.infer<typeof retrievalRequestSchema>
export type RetrievalResult = z.infer<typeof retrievalResultSchema>
export type RetrievalResults = z.infer<typeof retrievalResultsSchema>
