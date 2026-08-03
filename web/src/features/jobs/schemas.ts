import { z } from 'zod'

export const jobStatusSchema = z.enum([
  'pending',
  'queued',
  'running',
  'completed',
  'succeeded',
  'failed',
  'cancelled',
])

export const jobSummarySchema = z.object({
  id: z.uuid(),
  document_id: z.uuid().optional(),
  index_generation_id: z.uuid().optional(),
  status: jobStatusSchema,
  action_label: z.string(),
  target_type: z.string(),
  target_display_name: z.string(),
  attempts: z.number().int().nonnegative(),
  error_message: z.string().optional().default(''),
  created_at: z.string(),
  updated_at: z.string(),
})

export const jobSummaryPageSchema = z.object({
  items: z.array(jobSummarySchema),
  next_cursor: z.string().nullable(),
})
