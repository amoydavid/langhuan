import { z } from 'zod'
import { providerCommonFields, timeoutSchema } from './common'

export const dashScopeProviderSchema = z
  .object({
    ...providerCommonFields,
    provider: z.literal('dashscope'),
    timeout_seconds: timeoutSchema,
    api_key: z.string(),
  })
  .strict()
