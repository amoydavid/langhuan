import { z } from 'zod'
import i18n from '@/lib/i18n'
import { providerCommonFields, timeoutSchema } from './common'

export const ollamaProviderSchema = z
  .object({
    ...providerCommonFields,
    scope: z.literal('platform'),
    provider: z.literal('ollama'),
    base_url: z.url({
      error: () => i18n.t('models.schemas.ollamaUrlInvalid'),
    }),
    timeout_seconds: timeoutSchema,
  })
  .strict()
