import type { z } from 'zod'
import type {
  jobStatusSchema,
  jobSummaryPageSchema,
  jobSummarySchema,
} from './schemas'

export type JobStatus = z.infer<typeof jobStatusSchema>
export type JobSummary = z.infer<typeof jobSummarySchema>
export type JobSummaryPage = z.infer<typeof jobSummaryPageSchema>

export type JobListFilters = {
  document_id?: string
  status?: JobStatus
  cursor?: string
  limit?: number
}
