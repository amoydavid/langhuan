import { describe, expect, it } from 'vitest'
import { readWebSourceConfig, webSourceHealth } from './config'
import type { WebSource } from './types'

const baseSource: WebSource = {
  id: '60000000-0000-4000-8000-000000000006',
  workspace_id: '30000000-0000-4000-8000-000000000003',
  knowledge_base_id: '20000000-0000-4000-8000-000000000002',
  kind: 'web',
  name: '产品文档站',
  status: 'active',
  config: {},
  sync_cursor: {},
  next_sync_at: null,
  sync_requested_force: false,
  sync_last_result: {},
  created_at: '2026-09-16T00:00:00Z',
  updated_at: '2026-09-16T00:00:00Z',
}

describe('readWebSourceConfig', () => {
  it('falls back to backend defaults for an empty config', () => {
    const view = readWebSourceConfig({})
    expect(view).toMatchObject({
      mode: 'crawl',
      seedURL: '',
      urls: [],
      maxPages: 500,
      maxDepth: 3,
      respectRobots: true,
      engine: 'http',
      extractMode: 'readability',
      fallbackToFull: true,
      cron: '',
      onDelete: 'keep',
    })
  })

  it('reads a full crawl config with engine and connection', () => {
    const view = readWebSourceConfig({
      mode: 'crawl',
      crawl: {
        seed_url: 'https://docs.example.com/',
        max_pages: 120,
        max_depth: 2,
        include_paths: ['/docs/**'],
        exclude_paths: ['/admin/*'],
        allow_subdomains: true,
        respect_robots: false,
        request_delay_ms: 250,
      },
      fetch: { engine: 'render', connection_id: 'abc' },
      extract: { mode: 'full', fallback_to_full: false },
      sync: { cron: '0 0 * * *' },
      on_delete: 'remove',
    })
    expect(view).toMatchObject({
      mode: 'crawl',
      seedURL: 'https://docs.example.com/',
      maxPages: 120,
      maxDepth: 2,
      includePaths: ['/docs/**'],
      excludePaths: ['/admin/*'],
      allowSubdomains: true,
      respectRobots: false,
      requestDelayMs: 250,
      engine: 'render',
      connectionID: 'abc',
      extractMode: 'full',
      fallbackToFull: false,
      cron: '0 0 * * *',
      onDelete: 'remove',
    })
  })

  it('reads sitemap and urls mode identity keys', () => {
    expect(
      readWebSourceConfig({
        mode: 'sitemap',
        sitemap: { url: 'https://example.com/sitemap.xml' },
      }).sitemapURL
    ).toBe('https://example.com/sitemap.xml')
    expect(
      readWebSourceConfig({ mode: 'urls', urls: ['https://a.example.com/'] })
        .urls
    ).toEqual(['https://a.example.com/'])
  })

  it('never crashes on malformed shapes', () => {
    const view = readWebSourceConfig({
      mode: 42,
      crawl: 'not-an-object',
      urls: 'nope',
      fetch: null,
      sync: { cron: 7 },
    })
    expect(view.mode).toBe('crawl')
    expect(view.urls).toEqual([])
    expect(view.cron).toBe('')
    expect(view.engine).toBe('http')
  })
})

describe('webSourceHealth', () => {
  it('reports disabled before any sync result', () => {
    expect(webSourceHealth({ ...baseSource, status: 'disabled' })).toBe(
      'disabled'
    )
  })

  it('derives partial/failed/ok from the last sync result', () => {
    expect(
      webSourceHealth({
        ...baseSource,
        sync_last_result: { status: 'partial' },
      })
    ).toBe('partial')
    expect(
      webSourceHealth({
        ...baseSource,
        sync_last_result: { status: 'failed' },
      })
    ).toBe('failed')
    expect(
      webSourceHealth({
        ...baseSource,
        sync_last_result: { status: 'succeeded' },
      })
    ).toBe('ok')
  })

  it('treats a never-synced active source as ok', () => {
    expect(webSourceHealth(baseSource)).toBe('ok')
  })
})
