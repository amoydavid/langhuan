import { z } from 'zod'
import {
  optionalURLSchema,
  providerCommonFields,
  timeoutSchema,
} from './common'

export const arkProviderSchema = z
  .object({
    ...providerCommonFields,
    provider: z.literal('ark'),
    base_url: optionalURLSchema,
    region: z.string().trim().min(1),
    auth_mode: z.enum(['api_key', 'ak_sk']),
    timeout_seconds: timeoutSchema,
    retry_times: z.number().int().min(0).max(5),
    api_key: z.string().optional(),
    access_key: z.string().optional(),
    secret_key: z.string().optional(),
  })
  .strict()
