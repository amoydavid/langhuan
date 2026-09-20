import { describe, expect, it } from 'vitest'
import {
  buildCreatePayload,
  buildUpdatePayload,
  engineConnectionProviders,
  engineRequiresConnection,
  parseURLLines,
  partitionDuplicateURLs,
  type WebSourceFormValues,
  webSourceCreateDefaults,
  webSourceFormSchema,
} from './form-schema'

function validValues(
  overrides: Partial<WebSourceFormValues> = {}
): WebSourceFormValues {
  return {
    ...webSourceCreateDefaults(),
    name: '产品文档站',
    mode: 'crawl',
    seed_url: 'https://docs.example.com/',
    ...overrides,
  }
}

describe('webSourceFormSchema', () => {
  it('accepts a minimal crawl source', () => {
    const result = webSourceFormSchema.safeParse(validValues())
    expect(result.success).toBe(true)
  })

  it('requires the seed URL only in crawl mode', () => {
    const missing = webSourceFormSchema.safeParse({
      ...validValues(),
      seed_url: '',
    })
    expect(missing.success).toBe(false)
    if (!missing.success) {
      expect(missing.error.issues[0]?.path).toEqual(['seed_url'])
    }

    const sitemap = webSourceFormSchema.safeParse({
      ...validValues(),
      mode: 'sitemap',
      seed_url: '',
      sitemap_url: 'https://example.com/sitemap.xml',
    })
    expect(sitemap.success).toBe(true)
  })

  it('requires a parseable http(s) URL for the seed', () => {
    const result = webSourceFormSchema.safeParse({
      ...validValues(),
      seed_url: 'not-a-url',
    })
    expect(result.success).toBe(false)
  })

  it('rejects urls mode without any valid URL line', () => {
    const result = webSourceFormSchema.safeParse({
      ...validValues(),
      mode: 'urls',
      seed_url: '',
      urls_text: 'ftp://example.com/x\n\n',
    })
    expect(result.success).toBe(false)
  })

  it('requires a connection when the engine needs one', () => {
    const missing = webSourceFormSchema.safeParse({
      ...validValues(),
      engine: 'firecrawl',
    })
    expect(missing.success).toBe(false)
    if (!missing.success) {
      expect(missing.error.issues[0]?.path).toEqual(['connection_id'])
    }
    const provided = webSourceFormSchema.safeParse({
      ...validValues(),
      engine: 'firecrawl',
      connection_id: '50000000-0000-4000-8000-000000000005',
    })
    expect(provided.success).toBe(true)
  })

  it('requires a 5-field cron when the schedule is enabled', () => {
    const empty = webSourceFormSchema.safeParse({
      ...validValues(),
      sync_enabled: true,
      cron: '',
    })
    expect(empty.success).toBe(false)

    const malformed = webSourceFormSchema.safeParse({
      ...validValues(),
      sync_enabled: true,
      cron: '0 6',
    })
    expect(malformed.success).toBe(false)

    const valid = webSourceFormSchema.safeParse({
      ...validValues(),
      sync_enabled: true,
      cron: '0 */6 * * *',
    })
    expect(valid.success).toBe(true)
  })

  it('validates numeric fields only when non-empty', () => {
    const empty = webSourceFormSchema.safeParse({
      ...validValues(),
      max_pages: '',
      max_depth: '',
      request_delay_ms: '',
    })
    expect(empty.success).toBe(true)

    const invalid = webSourceFormSchema.safeParse({
      ...validValues(),
      max_pages: '-5',
      request_delay_ms: 'abc',
    })
    expect(invalid.success).toBe(false)
  })
})

describe('parseURLLines', () => {
  it('trims lines, drops blanks and rejects non-http(s) URLs', () => {
    const result = parseURLLines(
      ' https://a.example.com/x \n\nhttps://b.example.com/y\nftp://c.example.com/z\nnot a url'
    )
    expect(result.valid).toEqual([
      'https://a.example.com/x',
      'https://b.example.com/y',
    ])
    expect(result.invalid).toEqual(['ftp://c.example.com/z', 'not a url'])
  })

  it('dedupes exact duplicates within the added lines', () => {
    const result = parseURLLines(
      'https://a.example.com/x\nhttps://a.example.com/x'
    )
    expect(result.valid).toEqual(['https://a.example.com/x'])
  })
})

describe('partitionDuplicateURLs', () => {
  it('splits fresh lines from duplicates against the kept list', () => {
    const result = partitionDuplicateURLs(
      ['https://a.example.com/1', 'https://a.example.com/2'],
      ['https://a.example.com/1']
    )
    expect(result.fresh).toEqual(['https://a.example.com/2'])
    expect(result.duplicates).toEqual(['https://a.example.com/1'])
  })

  it('lets a removed URL be re-added without being flagged', () => {
    // pricing 已被勾选移除 → 不在保留清单 → 新增行视为全新 URL。
    const result = partitionDuplicateURLs(['https://a.example.com/1'], [])
    expect(result.fresh).toEqual(['https://a.example.com/1'])
    expect(result.duplicates).toEqual([])
  })
})

describe('payload builders', () => {
  it('builds a create payload matching the REST contract', () => {
    const payload = buildCreatePayload(
      validValues({
        max_pages: '120',
        exclude_paths: '/tags/*, /admin/**',
        allow_subdomains: true,
        request_delay_ms: '200',
        sync_enabled: true,
        cron: '0 */6 * * *',
      })
    )
    expect(payload.name).toBe('产品文档站')
    expect(payload.config).toMatchObject({
      mode: 'crawl',
      crawl: {
        seed_url: 'https://docs.example.com/',
        max_pages: 120,
        exclude_paths: ['/tags/*', '/admin/**'],
        allow_subdomains: true,
        respect_robots: true,
        request_delay_ms: 200,
      },
      fetch: { engine: 'http' },
      extract: { mode: 'readability', fallback_to_full: true },
      sync: { cron: '0 */6 * * *' },
      on_delete: 'keep',
    })
    expect(payload.config.urls as string[] | undefined).toBeUndefined()
    expect(payload.config.sitemap as unknown).toBeUndefined()
  })

  it('omits unset optional fields instead of sending empty values', () => {
    const payload = buildCreatePayload(
      validValues({ max_pages: '', max_depth: '', request_delay_ms: '' })
    )
    const crawl = payload.config.crawl as Record<string, unknown>
    expect('max_pages' in crawl).toBe(false)
    expect('max_depth' in crawl).toBe(false)
    expect('request_delay_ms' in crawl).toBe(false)
    expect('include_paths' in crawl).toBe(false)
    expect('sync' in payload.config).toBe(false)
  })

  it('builds a sitemap create payload with the sitemap object', () => {
    const payload = buildCreatePayload(
      validValues({
        mode: 'sitemap',
        seed_url: '',
        sitemap_url: 'https://example.com/sitemap.xml',
      })
    )
    expect(payload.config.sitemap).toEqual({
      url: 'https://example.com/sitemap.xml',
    })
  })

  it('builds a urls create payload with parsed lines', () => {
    const payload = buildCreatePayload(
      validValues({
        mode: 'urls',
        seed_url: '',
        urls_text: 'https://example.com/a\nhttps://example.com/b\nbroken-line',
      })
    )
    expect(payload.config.urls).toEqual([
      'https://example.com/a',
      'https://example.com/b',
    ])
  })

  it('never includes immutable keys in the update payload', () => {
    const payload = buildUpdatePayload(validValues({ engine: 'firecrawl' }))
    const config = payload.config
    expect('mode' in config).toBe(false)
    expect('fetch' in config).toBe(false)
    expect('sitemap' in config).toBe(false)
    expect('urls' in config).toBe(false)
    const crawl = config.crawl as Record<string, unknown>
    expect('seed_url' in crawl).toBe(false)
  })

  it('clears the cron explicitly when the schedule is turned off', () => {
    const payload = buildUpdatePayload(
      validValues({ sync_enabled: false, cron: '' })
    )
    expect(payload.config.sync).toEqual({ cron: '' })
  })
})

describe('edit defaults round-trip', () => {
  it('projects a config view back into editable values', () => {
    const values = webSourceCreateDefaults()
    values.name = '帮助中心'
    values.mode = 'urls'
    values.urls_text = 'https://example.com/help\nhttps://example.com/pricing'
    values.include_paths = '/a, /b'
    const payload = buildCreatePayload(values)
    expect((payload.config.urls as string[]).length).toBe(2)
    expect(payload.name).toBe('帮助中心')
  })
})

describe('engine helpers', () => {
  it('maps engines to their connection providers', () => {
    expect(engineRequiresConnection('http')).toBe(false)
    expect(engineRequiresConnection('auto')).toBe(true)
    expect(engineConnectionProviders('render')).toEqual(['browser'])
    expect(engineConnectionProviders('firecrawl')).toEqual(['firecrawl'])
    expect(engineConnectionProviders('jina')).toEqual(['jina'])
    expect(engineConnectionProviders('auto')).toEqual([
      'browser',
      'firecrawl',
      'jina',
    ])
    expect(engineConnectionProviders('http')).toEqual([])
  })
})
