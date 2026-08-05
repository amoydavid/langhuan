import { z } from 'zod'
import i18n from '@/lib/i18n'
import { providerCommonFields, timeoutSchema } from './common'

export const rerankCompatibleProviderSchema = z
  .object({
    ...providerCommonFields,
    provider: z.literal('rerank_compatible'),
    base_url: z.string().trim(),
    endpoint_path: z
      .string()
      .trim()
      .min(1)
      .startsWith('/', {
        error: () => i18n.t('models.schemas.endpointPathMustStartWithSlash'),
      })
      .default('/v1/rerank'),
    timeout_seconds: timeoutSchema,
    retry_times: z.number().int().min(0).max(3),
    api_key: z.string(),
    custom_headers: z.string(),
  })
  .strict()
