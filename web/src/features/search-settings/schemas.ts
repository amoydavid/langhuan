import { z } from 'zod'

export const workspaceSearchSettingsSchema = z.object({
  workspace_id: z.uuid(),
  rerank: z
    .object({
      model_id: z.uuid(),
      provider_id: z.uuid(),
      model_name: z.string(),
      candidate_top_k: z.number().int().min(50).max(200),
      failure_mode: z.enum(['fallback', 'fail']),
    })
    .nullable(),
  updated_at: z.string(),
})
