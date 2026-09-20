import type {
  WebSource,
  WebSourceDeletePolicy,
  WebSourceFetchEngine,
  WebSourceMode,
} from './types'

// 本文件把源 config 的 jsonb 原始 map 投影成 UI 可用的 typed 只读视图：
// 与后端 value.ParseWebSourceConfig 同名键对齐，但只做宽松读取（展示层不得因
// 形状异常崩溃，异常一律回退缺省值），不承担校验职责（校验在表单 schema 与后端）。

export type WebSourceConfigView = {
  mode: WebSourceMode
  /** seedURL 仅 mode=crawl 有值。 */
  seedURL: string
  /** sitemapURL 仅 mode=sitemap 有值。 */
  sitemapURL: string
  /** urls 仅 mode=urls 有值（后端已规范化）。 */
  urls: string[]
  maxPages: number
  maxDepth: number
  includePaths: string[]
  excludePaths: string[]
  allowSubdomains: boolean
  respectRobots: boolean
  requestDelayMs: number
  engine: WebSourceFetchEngine
  /** connectionID 为空串表示未设置（http 引擎恒为空）。 */
  connectionID: string
  extractMode: 'readability' | 'full'
  fallbackToFull: boolean
  /** cron 为空串表示无定时（仅手动同步）。 */
  cron: string
  onDelete: WebSourceDeletePolicy
}

/** 与后端 value 包缺省值对齐（web_source_config.go）。 */
export const defaultMaxPages = 500
export const defaultMaxDepth = 3

function asRecord(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
}

function asString(value: unknown, fallback = ''): string {
  return typeof value === 'string' ? value : fallback
}

function asNumber(value: unknown, fallback: number): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : fallback
}

function asBool(value: unknown, fallback: boolean): boolean {
  return typeof value === 'boolean' ? value : fallback
}

function asStringArray(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((item): item is string => typeof item === 'string')
    : []
}

const knownModes: WebSourceMode[] = ['crawl', 'sitemap', 'urls']
const knownEngines: WebSourceFetchEngine[] = [
  'auto',
  'http',
  'render',
  'firecrawl',
  'jina',
]

/** 把源 config map 投影成 typed 只读视图；缺失/非法键回退后端同款缺省值。 */
export function readWebSourceConfig(
  config: WebSource['config']
): WebSourceConfigView {
  const raw = asRecord(config)
  const crawl = asRecord(raw.crawl)
  const sitemap = asRecord(raw.sitemap)
  const fetchConfig = asRecord(raw.fetch)
  const extract = asRecord(raw.extract)
  const sync = asRecord(raw.sync)

  const rawMode = asString(raw.mode)
  const mode = knownModes.includes(rawMode as WebSourceMode)
    ? (rawMode as WebSourceMode)
    : 'crawl'
  const rawEngine = asString(fetchConfig.engine)
  const engine = knownEngines.includes(rawEngine as WebSourceFetchEngine)
    ? (rawEngine as WebSourceFetchEngine)
    : 'http'
  const rawExtractMode = asString(extract.mode)
  const rawOnDelete = asString(raw.on_delete)

  return {
    mode,
    seedURL: asString(crawl.seed_url),
    sitemapURL: asString(sitemap.url),
    urls: asStringArray(raw.urls),
    maxPages: asNumber(crawl.max_pages, defaultMaxPages),
    maxDepth: asNumber(crawl.max_depth, defaultMaxDepth),
    includePaths: asStringArray(crawl.include_paths),
    excludePaths: asStringArray(crawl.exclude_paths),
    allowSubdomains: asBool(crawl.allow_subdomains, false),
    respectRobots: asBool(crawl.respect_robots, true),
    requestDelayMs: asNumber(crawl.request_delay_ms, 0),
    engine,
    connectionID: asString(fetchConfig.connection_id),
    extractMode:
      rawExtractMode === 'full' || rawExtractMode === 'readability'
        ? rawExtractMode
        : 'readability',
    fallbackToFull: asBool(extract.fallback_to_full, true),
    cron: asString(sync.cron),
    onDelete:
      rawOnDelete === 'keep' || rawOnDelete === 'remove'
        ? (rawOnDelete as WebSourceDeletePolicy)
        : 'keep',
  }
}

export type WebSourceHealth = 'ok' | 'partial' | 'failed' | 'disabled'

/** 源状态徽标推导：停用优先；否则看最近一次同步结果（未同步视为正常）。 */
export function webSourceHealth(source: WebSource): WebSourceHealth {
  if (source.status === 'disabled') return 'disabled'
  const status = source.sync_last_result.status
  if (status === 'failed') return 'failed'
  if (status === 'partial') return 'partial'
  return 'ok'
}
