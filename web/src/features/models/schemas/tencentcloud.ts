import { z } from 'zod'
import i18n from '@/lib/i18n'
import { providerCommonFields } from './common'

export const tencentCloudProviderSchema = z
  .object({
    ...providerCommonFields,
    provider: z.literal('tencentcloud'),
    region: z
      .string()
      .trim()
      .min(1, { error: () => i18n.t('models.schemas.regionRequired') }),
    secret_id: z.string(),
    secret_key: z.string(),
  })
  .strict()
