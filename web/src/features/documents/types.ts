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
  | 'pending'
  | 'queued'
  | 'running'
  | 'completed'
  | 'succeeded'
  | 'failed'
  | 'cancelled'

export type Document = z.infer<typeof documentResponseSchema>

// DocumentAsset 是 PDF 等解析产出的图片资产（GET /documents/:id/assets 返回）。
export type DocumentAsset = {
  id: string
  document_id: string
  revision_id: string
  original_ref: string
  public_url: string
  mime_type: string
  sha256: string
  size_bytes: number
  metadata: Record<string, unknown>
  created_at: string
}

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
