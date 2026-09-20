import { z } from 'zod'

export const sourceProviderSchema = z.enum([
  'feishu',
  'firecrawl',
  'jina',
  'browser',
])

// 后端 workspace_source_connections 的 provider CHECK 只允许这四个值，
// 响应侧收窄为联合类型是安全的（T15/T16 按 provider 过滤连接下拉）。
const firecrawlEndpointPattern = /^https?:\/\/\S+$/u
const cdpEndpointPattern = /^wss?:\/\/\S+$/u

export const sourceConnectionResponseSchema = z.object({
  id: z.uuid(),
  workspace_id: z.uuid(),
  provider: sourceProviderSchema,
  name: z.string(),
  // 后端 DTO 恒有 endpoint_url（feishu/jina 恒为空串），无凭证泄漏。
  endpoint_url: z.string(),
  app_id: z.string(),
  status: z.enum(['active', 'disabled']),
  created_at: z.string(),
  updated_at: z.string(),
})

export const sourceConnectionListResponseSchema = z.array(
  sourceConnectionResponseSchema
)

// SourceConnectionTestResult：HTTP 恒 200，ok=false 表达探测失败（message 用户可读）。
export const sourceConnectionTestResultSchema = z.object({
  ok: z.boolean(),
  provider: z.string(),
  duration_ms: z.number().int(),
  message: z.string(),
})

// POST /source-connections 的 wire 契约：config 仅接受 endpoint_url
// （firecrawl 可选 http/https、browser 必填 ws/wss 的方案校验由后端按 provider 执行）。
export const createSourceConnectionSchema = z.object({
  provider: sourceProviderSchema,
  name: z.string().trim().min(1).max(64),
  app_id: z.string().trim().min(1).max(128).optional(),
  app_secret: z.string().min(1).max(256).optional(),
  config: z.object({ endpoint_url: z.string().min(1) }).optional(),
})

// 新建连接表单值：单张表单承载四种 provider，字段扁平保留（隐藏字段不提交），
// superRefine 按 provider 切换校验规则（与后端 NewSourceConnection 同口径）。
export const createSourceConnectionFormSchema = z
  .object({
    provider: sourceProviderSchema,
    name: z.string().trim().min(1, '应用名称不能为空').max(64),
    // 仅 feishu 使用；其它 provider 提交前丢弃（后端对多余 app_id 直接 400）。
    app_id: z.string().trim().max(128),
    // feishu=App Secret、firecrawl/jina=API Key 必填；browser=Token 可选。
    app_secret: z.string().max(256),
    // firecrawl 可选 http/https；browser 必填 ws/wss。
    endpoint_url: z.string().trim().max(2048),
  })
  .superRefine((value, ctx) => {
    if (value.provider === 'feishu') {
      if (value.app_id === '') {
        ctx.addIssue({
          code: 'custom',
          path: ['app_id'],
          message: 'App ID 不能为空',
        })
      }
      if (value.app_secret === '') {
        ctx.addIssue({
          code: 'custom',
          path: ['app_secret'],
          message: 'App Secret 不能为空',
        })
      }
      return
    }
    if (value.provider === 'browser') {
      if (value.endpoint_url === '') {
        ctx.addIssue({
          code: 'custom',
          path: ['endpoint_url'],
          message: 'Endpoint URL 不能为空',
        })
      } else if (!cdpEndpointPattern.test(value.endpoint_url)) {
        ctx.addIssue({
          code: 'custom',
          path: ['endpoint_url'],
          message: 'CDP 端点仅支持 ws/wss（如 ws://host:9222）',
        })
      }
      return
    }
    // firecrawl / jina：API Key 必填。
    if (value.app_secret === '') {
      ctx.addIssue({
        code: 'custom',
        path: ['app_secret'],
        message: 'API Key 不能为空',
      })
    }
    if (
      value.provider === 'firecrawl' &&
      value.endpoint_url !== '' &&
      !firecrawlEndpointPattern.test(value.endpoint_url)
    ) {
      ctx.addIssue({
        code: 'custom',
        path: ['endpoint_url'],
        message: 'Endpoint URL 仅支持 http/https',
      })
    }
  })

export type SourceConnectionFormValues = z.infer<
  typeof createSourceConnectionFormSchema
>

export type CreateSourceConnectionPayload = z.infer<
  typeof createSourceConnectionSchema
>

/** 表单值 → POST /source-connections 请求体：只携带当前 provider 真正接受的字段。 */
export function toCreateSourceConnectionInput(
  values: SourceConnectionFormValues
): CreateSourceConnectionPayload {
  const name = values.name.trim()
  const endpointURL = values.endpoint_url.trim()
  switch (values.provider) {
    case 'feishu':
      return {
        provider: 'feishu',
        name,
        app_id: values.app_id.trim(),
        app_secret: values.app_secret,
      }
    case 'firecrawl':
      return {
        provider: 'firecrawl',
        name,
        app_secret: values.app_secret,
        ...(endpointURL === ''
          ? {}
          : { config: { endpoint_url: endpointURL } }),
      }
    case 'jina':
      return { provider: 'jina', name, app_secret: values.app_secret }
    case 'browser':
      return {
        provider: 'browser',
        name,
        ...(values.app_secret === '' ? {} : { app_secret: values.app_secret }),
        config: { endpoint_url: endpointURL },
      }
  }
}

export const updateSourceConnectionSchema = z
  .object({
    name: z.string().trim().min(1).max(64).optional(),
    status: z.enum(['active', 'disabled']).optional(),
    app_secret: z.string().min(1).max(256).optional(),
  })
  .refine((value) => value.name || value.status || value.app_secret, {
    message: '至少提供一项更新字段',
  })
