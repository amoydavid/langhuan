import { z } from 'zod'
import i18n from '@/lib/i18n'

// 到期方式判别联合：days（固定天数）或 never（不限期）。
const apiKeyExpirationSchema = z.discriminatedUnion('type', [
  z.object({
    type: z.literal('days'),
    days: z
      .number({ error: () => i18n.t('apiKeys.schemas.daysRequired') })
      .int({ error: () => i18n.t('apiKeys.schemas.daysInteger') })
      .min(1, { error: () => i18n.t('apiKeys.schemas.daysMin') })
      .max(365, { error: () => i18n.t('apiKeys.schemas.daysMax') }),
  }),
  z.object({
    type: z.literal('never'),
  }),
])

// 创建 API Key 的表单 schema，字段对齐后端 createAPIKeyRequest。
export const apiKeyCreateSchema = z.object({
  name: z
    .string({ error: () => i18n.t('apiKeys.schemas.nameRequired') })
    .trim()
    .min(1, { error: () => i18n.t('apiKeys.schemas.nameRequired') })
    .max(80, { error: () => i18n.t('apiKeys.schemas.nameTooLong') }),
  knowledge_base_ids: z
    .array(
      z.uuid({ error: () => i18n.t('apiKeys.schemas.invalidKnowledgeBaseId') })
    )
    .min(1, { error: () => i18n.t('apiKeys.schemas.knowledgeBaseRequired') }),
  scopes: z
    .array(
      z.enum([
        'knowledge_bases:write',
        'documents:read',
        'documents:write',
        'search:read',
      ])
    )
    .min(1, { error: () => i18n.t('apiKeys.schemas.scopeRequired') }),
  expiration: apiKeyExpirationSchema,
})

export type APIKeyCreateFormValues = z.infer<typeof apiKeyCreateSchema>

// 创建请求体，与后端 createAPIKeyRequest 的 JSON 形状一致。
export type APIKeyCreateInput = APIKeyCreateFormValues
