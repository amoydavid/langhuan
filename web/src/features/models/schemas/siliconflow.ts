import { z } from 'zod'
import i18n from '@/lib/i18n'
import { providerCommonFields, timeoutSchema } from './common'

const endpointPath = z
  .string()
  .trim()
  .min(1)
  .startsWith('/', {
    error: () => i18n.t('models.schemas.endpointPathMustStartWithSlash'),
  })

export const siliconFlowProviderSchema = z
  .object({
    ...providerCommonFields,
    provider: z.literal('siliconflow'),
    base_url: z.url({ error: () => i18n.t('models.schemas.validUrl') }),
    embedding_endpoint_path: endpointPath.default('/v1/embeddings'),
    rerank_endpoint_path: endpointPath.default('/v1/rerank'),
    timeout_seconds: timeoutSchema,
    retry_times: z.number().int().min(0).max(3),
    api_key: z.string(),
  })
  .strict()
