import type { z } from 'zod'
import type {
  documentKindSchema,
  documentResponseSchema,
  documentRevisionSummarySchema,
  documentStatusSchema,
} from './schemas'

export type DocumentKind = z.infer<typeof documentKindSchema>
export type DocumentStatus = z.infer<typeof documentStatusSchema>
export type DocumentRevisionSummary = z.infer<
  typeof documentRevisionSummarySchema
>

export type JobStatus =
  | 'queued'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'cancelled'

export type Document = z.infer<typeof documentResponseSchema>

export type Job = {
  id: string
  document_id: string
  type: string
  status: JobStatus
  attempts: number
  external_job_id: string
  payload: Record<string, unknown>
  error_message: string
  created_at: string
  updated_at: string
}

export type DocumentIngestResult = {
  document: Document
  job: Job
  deduped: boolean
}

export type UploadDocumentInput = {
  file: File
  title: string
  source_type: string
  dedupe: boolean
  parent_node_id?: string
  node_name?: string
}
