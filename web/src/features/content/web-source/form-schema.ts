import { z } from 'zod'
import i18n from '@/lib/i18n'
import {
  defaultMaxDepth,
  defaultMaxPages,
  type WebSourceConfigView,
} from './config'
import type { WebSourceMode } from './types'

// 添加/编辑网站源表单契约。URL 规范化由后端完成，本端只做格式预检
// （http/https 且可解析）；数字字段以字符串承载（避免空输入 NaN），由
// buildCreatePayload / buildUpdatePayload 转成后端 config 形状。

const positiveIntPattern = /^\d+$/u

/** 引擎非 http 时必须选择对应 provider 的连接（后端同款约束）。 */
export function engineRequiresConnection(engine: string): boolean {
  return engine !== 'http'
}

/** 引擎对应的连接 provider；auto 可升级到任一非 http 引擎。 */
export function engineConnectionProviders(engine: string): string[] {
  switch (engine) {
    case 'render':
      return ['browser']
    case 'firecrawl':
      return ['firecrawl']
    case 'jina':
      return ['jina']
    case 'auto':
      return ['browser', 'firecrawl', 'jina']
    default:
      return []
  }
}

export function isValidWebURL(value: string): boolean {
  try {
    const url = new URL(value)
    return url.protocol === 'http:' || url.protocol === 'https:'
  } catch {
    return false
  }
}

export type URLLineParseResult = {
  valid: string[]
  invalid: string[]
}

/** 逐行解析 URL 文本：trim、去空行、http/https 预检。 */
export function parseURLLines(text: string): URLLineParseResult {
  const valid: string[] = []
  const invalid: string[] = []
  for (const rawLine of text.split(/\r?\n/u)) {
    const line = rawLine.trim()
    if (line === '') continue
    if (isValidWebURL(line)) {
      if (!valid.includes(line)) valid.push(line)
    } else {
      invalid.push(line)
    }
  }
  return { valid, invalid }
}

/**
 * 把解析出的有效行按「是否已在清单中」分组：与现有（勾选保留的）URL 重复的行
 * 在保存前即剔除并在预览中提示，避免整体替换后出现重复项被后端 400 拒绝。
 * 重复判定基于保留后的清单——被取消勾选的 URL 允许通过新增行加回。
 */
export function partitionDuplicateURLs(
  valid: string[],
  keptExisting: string[]
): { fresh: string[]; duplicates: string[] } {
  const known = new Set(keptExisting)
  const fresh: string[] = []
  const duplicates: string[] = []
  for (const url of valid) {
    if (known.has(url)) {
      duplicates.push(url)
    } else {
      fresh.push(url)
      known.add(url)
    }
  }
  return { fresh, duplicates }
}

function pathsFromText(text: string): string[] {
  return text
    .split(/[,，\s]+/u)
    .map((item) => item.trim())
    .filter((item) => item.length > 0)
}

export const webSourceFormSchema = z
  .object({
    name: z
      .string()
      .trim()
      .min(1, { message: i18n.t('webSource.schema.nameRequired') }),
    mode: z.enum(['crawl', 'sitemap', 'urls']),
    seed_url: z.string().trim(),
    sitemap_url: z.string().trim(),
    urls_text: z.string(),
    max_pages: z.string().trim(),
    max_depth: z.string().trim(),
    include_paths: z.string(),
    exclude_paths: z.string(),
    allow_subdomains: z.boolean(),
    respect_robots: z.boolean(),
    request_delay_ms: z.string().trim(),
    engine: z.enum(['auto', 'http', 'render', 'firecrawl', 'jina']),
    connection_id: z.string(),
    extract_mode: z.enum(['readability', 'full']),
    fallback_to_full: z.boolean(),
    sync_enabled: z.boolean(),
    cron: z.string().trim(),
    on_delete: z.enum(['keep', 'remove']),
  })
  .superRefine((values, context) => {
    switch (values.mode) {
      case 'crawl':
        if (values.seed_url === '') {
          context.addIssue({
            code: 'custom',
            message: i18n.t('webSource.schema.seedUrlRequired'),
            path: ['seed_url'],
          })
        } else if (!isValidWebURL(values.seed_url)) {
          context.addIssue({
            code: 'custom',
            message: i18n.t('webSource.schema.invalidUrl'),
            path: ['seed_url'],
          })
        }
        break
      case 'sitemap':
        if (values.sitemap_url === '') {
          context.addIssue({
            code: 'custom',
            message: i18n.t('webSource.schema.sitemapUrlRequired'),
            path: ['sitemap_url'],
          })
        } else if (!isValidWebURL(values.sitemap_url)) {
          context.addIssue({
            code: 'custom',
            message: i18n.t('webSource.schema.invalidUrl'),
            path: ['sitemap_url'],
          })
        }
        break
      case 'urls': {
        const { valid } = parseURLLines(values.urls_text)
        if (valid.length === 0) {
          context.addIssue({
            code: 'custom',
            message: i18n.t('webSource.schema.urlsRequired'),
            path: ['urls_text'],
          })
        }
        break
      }
    }

    if (values.mode === 'crawl') {
      for (const key of ['max_pages', 'max_depth'] as const) {
        const raw = values[key]
        if (raw !== '' && !positiveIntPattern.test(raw)) {
          context.addIssue({
            code: 'custom',
            message: i18n.t('webSource.schema.positiveIntRequired'),
            path: [key],
          })
        }
      }
    }

    const delay = values.request_delay_ms
    if (delay !== '' && !positiveIntPattern.test(delay)) {
      context.addIssue({
        code: 'custom',
        message: i18n.t('webSource.schema.nonNegativeIntRequired'),
        path: ['request_delay_ms'],
      })
    }

    if (
      engineRequiresConnection(values.engine) &&
      values.connection_id === ''
    ) {
      context.addIssue({
        code: 'custom',
        message: i18n.t('webSource.schema.connectionRequired'),
        path: ['connection_id'],
      })
    }

    if (values.sync_enabled) {
      if (values.cron === '') {
        context.addIssue({
          code: 'custom',
          message: i18n.t('webSource.schema.cronRequired'),
          path: ['cron'],
        })
      } else if (values.cron.split(/\s+/u).length !== 5) {
        context.addIssue({
          code: 'custom',
          message: i18n.t('webSource.schema.cronFormat'),
          path: ['cron'],
        })
      }
    }
  })

export type WebSourceFormValues = z.infer<typeof webSourceFormSchema>

/** 创建表单缺省值（与后端缺省对齐）。 */
export function webSourceCreateDefaults(): WebSourceFormValues {
  return {
    name: '',
    mode: 'crawl',
    seed_url: '',
    sitemap_url: '',
    urls_text: '',
    max_pages: String(defaultMaxPages),
    max_depth: String(defaultMaxDepth),
    include_paths: '',
    exclude_paths: '',
    allow_subdomains: false,
    respect_robots: true,
    request_delay_ms: '',
    engine: 'http',
    connection_id: '',
    extract_mode: 'readability',
    fallback_to_full: true,
    sync_enabled: false,
    cron: '',
    on_delete: 'keep',
  }
}

/** 编辑表单初值：由源 config 视图投影。 */
export function webSourceEditDefaults(
  name: string,
  view: WebSourceConfigView
): WebSourceFormValues {
  return {
    name,
    mode: view.mode,
    seed_url: view.seedURL,
    sitemap_url: view.sitemapURL,
    urls_text: view.urls.join('\n'),
    max_pages: String(view.maxPages),
    max_depth: String(view.maxDepth),
    include_paths: view.includePaths.join(', '),
    exclude_paths: view.excludePaths.join(', '),
    allow_subdomains: view.allowSubdomains,
    respect_robots: view.respectRobots,
    request_delay_ms:
      view.requestDelayMs > 0 ? String(view.requestDelayMs) : '',
    engine: view.engine,
    connection_id: view.connectionID,
    extract_mode: view.extractMode,
    fallback_to_full: view.fallbackToFull,
    sync_enabled: view.cron !== '',
    cron: view.cron,
    on_delete: view.onDelete,
  }
}

function optionalPositiveInt(raw: string): number | undefined {
  if (raw === '' || !positiveIntPattern.test(raw)) return undefined
  return Number(raw)
}

function crawlConfigForCreate(values: WebSourceFormValues) {
  const crawl: Record<string, unknown> = {}
  if (values.mode === 'crawl') crawl.seed_url = values.seed_url
  return withMutableCrawlKeys(values, crawl)
}

// crawl 子对象的可变键集合：礼貌性参数跨模式生效（spec §4.1），
// seed_url 仅 crawl 模式有效且创建后不可变。
function withMutableCrawlKeys(
  values: WebSourceFormValues,
  crawl: Record<string, unknown>
) {
  const maxPages = optionalPositiveInt(values.max_pages)
  if (maxPages !== undefined) crawl.max_pages = maxPages
  const maxDepth = optionalPositiveInt(values.max_depth)
  if (maxDepth !== undefined) crawl.max_depth = maxDepth
  const includePaths = pathsFromText(values.include_paths)
  if (includePaths.length > 0) crawl.include_paths = includePaths
  const excludePaths = pathsFromText(values.exclude_paths)
  if (excludePaths.length > 0) crawl.exclude_paths = excludePaths
  if (values.allow_subdomains) crawl.allow_subdomains = true
  crawl.respect_robots = values.respect_robots
  const delay = optionalPositiveInt(values.request_delay_ms)
  if (delay !== undefined) crawl.request_delay_ms = delay
  return crawl
}

/** 创建载荷：POST .../web-sources 的 body。 */
export function buildCreatePayload(values: WebSourceFormValues): {
  name: string
  config: Record<string, unknown>
} {
  const config: Record<string, unknown> = {
    mode: values.mode,
    crawl: crawlConfigForCreate(values),
    fetch: {
      engine: values.engine,
      ...(values.connection_id !== ''
        ? { connection_id: values.connection_id }
        : {}),
    },
    extract: {
      mode: values.extract_mode,
      fallback_to_full: values.fallback_to_full,
    },
    on_delete: values.on_delete,
  }
  if (values.mode === 'sitemap') {
    config.sitemap = { url: values.sitemap_url }
  }
  if (values.mode === 'urls') {
    config.urls = parseURLLines(values.urls_text).valid
  }
  if (values.sync_enabled) {
    config.sync = { cron: values.cron }
  }
  return { name: values.name, config }
}

/**
 * 编辑载荷：PATCH 语义。只发送可变键（crawl 去掉 seed_url；fetch 的
 * engine/connection_id、sitemap.url、mode 不可变故整体不出现），后端对
 * partial config 做深合并 + 全量重校验。urls 模式的页面清单由「管理 URL」
 * 对话框整体替换维护，编辑表单不携带 urls。
 */
export function buildUpdatePayload(values: WebSourceFormValues): {
  name: string
  config: Record<string, unknown>
} {
  const config: Record<string, unknown> = {
    crawl: withMutableCrawlKeys(values, {}),
    extract: {
      mode: values.extract_mode,
      fallback_to_full: values.fallback_to_full,
    },
    on_delete: values.on_delete,
  }
  if (values.sync_enabled) {
    config.sync = { cron: values.cron }
  } else {
    // 显式清空定时：sync.cron 为空串表示无定时。
    config.sync = { cron: '' }
  }
  return { name: values.name, config }
}

export function modeLabelKey(mode: WebSourceMode): string {
  return `webSource.mode.${mode}`
}
