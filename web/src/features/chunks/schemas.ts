import { z } from 'zod'

export const chunkRevisionStatusSchema = z.enum([
  'pending',
  'indexing',
  'ready',
  'failed',
])

export const chunkEditSourceSchema = z.enum(['system', 'user'])

export const chunkRevisionSchema = z.object({
  id: z.uuid(),
  chunk_id: z.uuid(),
  revision_no: z.number().int().positive(),
  base_revision_id: z.uuid().optional(),
  content: z.string(),
  context_header: z.string(),
  enabled: z.boolean(),
  status: chunkRevisionStatusSchema,
  edit_source: chunkEditSourceSchema,
  editor_user_id: z.uuid().optional(),
  editor_display_name: z.string().min(1),
  error_message: z.string().optional().default(''),
  created_at: z.string(),
  indexed_at: z.string().nullable().optional(),
})

export const chunkSchema = z.object({
  id: z.uuid(),
  workspace_id: z.uuid(),
  knowledge_base_id: z.uuid(),
  document_id: z.uuid(),
  document_revision_id: z.uuid(),
  chunk_set_id: z.uuid(),
  role: z.enum(['parent', 'child', 'flat']).optional(),
  parent_chunk_id: z.uuid().nullable().optional(),
  sequence: z.number().int().nonnegative(),
  source_content: z.string(),
  source_anchor: z.record(z.string(), z.unknown()),
  metadata: z.record(z.string(), z.unknown()),
  active_revision: chunkRevisionSchema.nullable().optional(),
  created_at: z.string(),
})

export const documentChunkPageSchema = z.object({
  generation_id: z.uuid(),
  document_revision_id: z.uuid(),
  chunk_set_id: z.uuid(),
  items: z.array(chunkSchema),
  next_cursor: z.string().nullable(),
})

export const chunkRevisionListSchema = z.array(chunkRevisionSchema)
