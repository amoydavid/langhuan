import { z } from 'zod'
import i18n from '@/lib/i18n'
import { arkProviderSchema } from './ark'
import { dashScopeProviderSchema } from './dashscope'
import { minerUProviderSchema } from './mineru'
import { ollamaProviderSchema } from './ollama'
import { openAIProviderSchema } from './openai'
import { rerankCompatibleProviderSchema } from './rerank-compatible'
import { siliconFlowProviderSchema } from './siliconflow'
import { tencentCloudProviderSchema } from './tencentcloud'

export {
  type ModelFormValues,
  modelFormSchema,
  type RerankModelFormValues,
  rerankModelFormSchema,
} from './common'

export const providerFormSchema = z
  .discriminatedUnion('provider', [
    openAIProviderSchema,
    arkProviderSchema,
    ollamaProviderSchema,
    dashScopeProviderSchema,
    tencentCloudProviderSchema,
    rerankCompatibleProviderSchema,
    siliconFlowProviderSchema,
    minerUProviderSchema,
  ])
  .superRefine((values, context) => {
    if (values.provider === 'openai') {
      if (
        values.mode === 'azure' &&
        (!values.base_url || !values.api_version)
      ) {
        context.addIssue({
          code: 'custom',
          path: ['base_url'],
          message: i18n.t('models.schemas.azureModeRequiresEndpoint'),
        })
      }
      if (values.mode === 'standard' && values.api_version) {
        context.addIssue({
          code: 'custom',
          path: ['api_version'],
          message: i18n.t('models.schemas.standardModeNoApiVersion'),
        })
      }
      if (values.replace_credentials && !values.api_key.trim()) {
        context.addIssue({
          code: 'custom',
          path: ['api_key'],
          message: i18n.t('models.schemas.apiKeyRequired'),
        })
      }
    }
    if (values.provider === 'ark' && values.replace_credentials) {
      if (values.auth_mode === 'api_key' && !values.api_key?.trim()) {
        context.addIssue({
          code: 'custom',
          path: ['api_key'],
          message: i18n.t('models.schemas.apiKeyRequired'),
        })
      }
      if (
        values.auth_mode === 'ak_sk' &&
        (!values.access_key?.trim() || !values.secret_key?.trim())
      ) {
        context.addIssue({
          code: 'custom',
          path: ['access_key'],
          message: i18n.t('models.schemas.akSkRequired'),
        })
      }
    }
    if (
      values.provider === 'dashscope' &&
      values.replace_credentials &&
      !values.api_key.trim()
    ) {
      context.addIssue({
        code: 'custom',
        path: ['api_key'],
        message: i18n.t('models.schemas.apiKeyRequired'),
      })
    }
    if (
      values.provider === 'siliconflow' &&
      values.replace_credentials &&
      !values.api_key.trim()
    ) {
      context.addIssue({
        code: 'custom',
        path: ['api_key'],
        message: i18n.t('models.schemas.apiKeyRequired'),
      })
    }
    if (
      values.provider === 'tencentcloud' &&
      values.replace_credentials &&
      (!values.secret_id.trim() || !values.secret_key.trim())
    ) {
      context.addIssue({
        code: 'custom',
        path: ['secret_id'],
        message: i18n.t('models.schemas.secretIdSkRequired'),
      })
    }
    if (
      values.provider === 'mineru' &&
      values.replace_credentials &&
      !values.token.trim()
    ) {
      context.addIssue({
        code: 'custom',
        path: ['token'],
        message: i18n.t('models.schemas.tokenRequired'),
      })
    }
  })

export type ProviderFormValues = z.infer<typeof providerFormSchema>
