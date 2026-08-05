import { z } from 'zod'
import i18n from '@/lib/i18n'
export const providerCommonFields = {
  scope: z.enum(['workspace', 'platform']),
  name: z
    .string()
    .trim()
    .regex(/^[a-z][a-z0-9_-]{0,63}$/, {
      error: () => i18n.t('models.schemas.providerNameLowercase'),
    }),
  display_name: z
    .string()
    .trim()
    .min(1, { error: () => i18n.t('models.schemas.displayNameRequired') })
    .max(255),
  description: z.string().trim().max(2000),
  replace_credentials: z.boolean(),
}

export const timeoutSchema = z
  .number({ error: () => i18n.t('models.schemas.timeoutRequired') })
  .int()
  .min(1)
  .max(600)

export const optionalURLSchema = z.union([
  z.literal(''),
  z.url({ error: () => i18n.t('models.schemas.validUrl') }),
])

export const modelFormSchema = z
  .object({
    name: z
      .string()
      .trim()
      .regex(/^[a-z][a-z0-9_-]{0,63}$/, {
        error: () => i18n.t('models.schemas.modelNameLowercase'),
      }),
    display_name: z
      .string()
      .trim()
      .min(1, { error: () => i18n.t('models.schemas.displayNameRequired') })
      .max(255),
    description: z.string().trim().max(2000),
    model_name: z
      .string()
      .trim()
      .min(1, { error: () => i18n.t('models.schemas.modelNameRequired') })
      .max(255),
    dimensions: z.union([
      z.literal(798),
      z.literal(1024),
      z.literal(2048),
      z.literal(3584),
    ]),
    batch_size: z.number().int().min(1).max(200),
    truncate: z.boolean(),
    keep_alive_seconds: z.number().int().min(0).max(86400).optional(),
  })
  .strict()

export type ModelFormValues = z.infer<typeof modelFormSchema>

export const rerankModelFormSchema = z
  .object({
    name: z
      .string()
      .trim()
      .regex(/^[a-z][a-z0-9_-]{0,63}$/, {
        error: () => i18n.t('models.schemas.modelNameLowercase'),
      }),
    display_name: z
      .string()
      .trim()
      .min(1, { error: () => i18n.t('models.schemas.displayNameRequired') })
      .max(255),
    description: z.string().trim().max(2000),
    model_name: z
      .string()
      .trim()
      .min(1, { error: () => i18n.t('models.schemas.modelNameRequired') })
      .max(255),
    max_documents: z
      .number()
      .int()
      .min(50, { error: () => i18n.t('models.schemas.maxDocumentsRange') })
      .max(200, { error: () => i18n.t('models.schemas.maxDocumentsRange') }),
    max_query_chars: z
      .number()
      .int()
      .min(256, { error: () => i18n.t('models.schemas.maxQueryCharsRange') })
      .max(4096, { error: () => i18n.t('models.schemas.maxQueryCharsRange') }),
    max_document_chars: z
      .number()
      .int()
      .min(512, { error: () => i18n.t('models.schemas.maxDocumentCharsRange') })
      .max(32768, {
        error: () => i18n.t('models.schemas.maxDocumentCharsRange'),
      }),
  })
  .strict()

export type RerankModelFormValues = z.infer<typeof rerankModelFormSchema>
