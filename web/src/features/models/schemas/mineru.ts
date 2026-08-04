import { z } from 'zod'
import { providerCommonFields } from './common'

export const minerUProviderSchema = z.object({
  ...providerCommonFields,
  provider: z.literal('mineru'),
  base_url: z.string().optional(),
  model_version: z.enum(['vlm', 'pipeline']).default('vlm'),
  token: z.string(),
})
