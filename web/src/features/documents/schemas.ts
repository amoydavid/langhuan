import { z } from 'zod'
import i18n from '@/lib/i18n'

export const documentUploadSchema = z.object({
  file: z.instanceof(File, {
    error: () => i18n.t('documents.uploadForm.fileRequired'),
  }),
  title: z
    .string()
    .trim()
    .min(1, { error: () => i18n.t('documents.uploadForm.titleRequired') }),
  source_type: z
    .string()
    .trim()
    .min(1, { error: () => i18n.t('documents.uploadForm.sourceTypeRequired') }),
  dedupe: z.boolean(),
})

export type DocumentUploadFormValues = z.infer<typeof documentUploadSchema>

export const documentKindSchema = z.enum(['file', 'faq', 'web'])
export const documentStatusSchema = z.enum([
  'pending',
  'processing',
  'ready',
  'failed',
  'deleting',
  'deleted',
])

export const parseWarningSchema = z.object({
  code: z.string(),
  message: z.string(),
})

export const documentRevisionSummarySchema = z.object({
  id: z.uuid(),
  revision_no: z.number().int().positive(),
  status: z.enum(['pending', 'parsing', 'ready', 'failed']),
  original_filename: z.string().optional(),
  file_type: z.string().optional(),
  content_type: z.string().optional(),
  sha256: z.string().optional(),
  size_bytes: z.number().int().nonnegative(),
  warnings: z.array(parseWarningSchema).optional(),
  created_at: z.string(),
})

export const documentResponseSchema = z.object({
  id: z.uuid(),
  workspace_id: z.uuid(),
  knowledge_base_id: z.uuid(),
  kind: documentKindSchema,
  title: z.string(),
  source_type: z.string(),
  source_uri: z.string().nullable().optional().default(null),
  status: documentStatusSchema,
  normalized_markdown: z.string().optional().default(''),
  metadata: z.record(z.string(), z.unknown()),
  faq_question_count: z.number().int().nonnegative().optional(),
  error_message: z.string().optional().default(''),
  created_at: z.string(),
  updated_at: z.string(),
  active_revision: documentRevisionSummarySchema.optional(),
})

export const documentListResponseSchema = z.array(documentResponseSchema)
