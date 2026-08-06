import { z } from 'zod'

export const sourceConnectionResponseSchema = z.object({
  id: z.uuid(),
  workspace_id: z.uuid(),
  provider: z.string(),
  name: z.string(),
  app_id: z.string(),
  status: z.enum(['active', 'disabled']),
  created_at: z.string(),
  updated_at: z.string(),
})

export const sourceConnectionListResponseSchema = z.array(
  sourceConnectionResponseSchema
)

export const createSourceConnectionSchema = z.object({
  provider: z.literal('feishu'),
  name: z.string().trim().min(1, '应用名称不能为空').max(64),
  app_id: z.string().trim().min(1, 'App ID 不能为空').max(128),
  app_secret: z.string().min(1, 'App Secret 不能为空').max(256),
})

export const updateSourceConnectionSchema = z
  .object({
    name: z.string().trim().min(1).max(64).optional(),
    status: z.enum(['active', 'disabled']).optional(),
    app_secret: z.string().min(1).max(256).optional(),
  })
  .refine((value) => value.name || value.status || value.app_secret, {
    message: '至少提供一项更新字段',
  })
