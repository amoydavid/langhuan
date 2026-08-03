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

// API Key 共享字段校验。创建与编辑复用同一组规则，保证前后端语义一致。
const apiKeyNameField = z
  .string({ error: () => i18n.t('apiKeys.schemas.nameRequired') })
  .trim()
  .min(1, { error: () => i18n.t('apiKeys.schemas.nameRequired') })
  .max(80, { error: () => i18n.t('apiKeys.schemas.nameTooLong') })

const apiKeyKnowledgeBaseIdsField = z
  .array(
    z.uuid({ error: () => i18n.t('apiKeys.schemas.invalidKnowledgeBaseId') })
  )
  .min(1, { error: () => i18n.t('apiKeys.schemas.knowledgeBaseRequired') })

const apiKeyScopesField = z
  .array(
    z.enum([
      'knowledge_bases:write',
      'documents:read',
      'documents:write',
      'search:read',
    ])
  )
  .min(1, { error: () => i18n.t('apiKeys.schemas.scopeRequired') })

// 创建 API Key 的表单 schema，字段对齐后端 createAPIKeyRequest。
export const apiKeyCreateSchema = z.object({
  name: apiKeyNameField,
  knowledge_base_ids: apiKeyKnowledgeBaseIdsField,
  scopes: apiKeyScopesField,
  expiration: apiKeyExpirationSchema,
})

export type APIKeyCreateFormValues = z.infer<typeof apiKeyCreateSchema>

// 创建请求体，与后端 createAPIKeyRequest 的 JSON 形状一致。
export type APIKeyCreateInput = APIKeyCreateFormValues

// 编辑 API Key 的表单 schema，字段对齐后端 updateAPIKeyRequest。
// 与创建同构，复用同一组校验规则。
export const apiKeyUpdateSchema = z.object({
  name: apiKeyNameField,
  knowledge_base_ids: apiKeyKnowledgeBaseIdsField,
  scopes: apiKeyScopesField,
  expiration: apiKeyExpirationSchema,
})

export type APIKeyUpdateFormValues = z.infer<typeof apiKeyUpdateSchema>

// 编辑请求体，与后端 updateAPIKeyRequest 的 JSON 形状一致。
export type APIKeyUpdateInput = APIKeyUpdateFormValues
