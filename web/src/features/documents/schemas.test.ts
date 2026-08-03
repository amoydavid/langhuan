import { describe, expect, it } from 'vitest'
import { documentResponseSchema } from './schemas'

describe('documentResponseSchema', () => {
  it('parses every Document kind with revision-local file metadata', () => {
    const parsed = documentResponseSchema.parse({
      id: '10000000-0000-4000-8000-000000000001',
      workspace_id: '20000000-0000-4000-8000-000000000002',
      knowledge_base_id: '30000000-0000-4000-8000-000000000003',
      kind: 'file',
      title: '指南.md',
      source_type: 'upload',
      source_uri: null,
      status: 'ready',
      normalized_markdown: '# 指南',
      metadata: {},
      error_message: '',
      created_at: '2026-07-31T00:00:00Z',
      updated_at: '2026-07-31T00:00:00Z',
      raw_storage_key: 'must-not-reach-the-web-model',
      active_revision: {
        id: '40000000-0000-4000-8000-000000000004',
        revision_no: 2,
        status: 'ready',
        original_filename: 'guide.md',
        file_type: 'markdown',
        content_type: 'text/markdown',
        sha256: 'abc',
        size_bytes: 12,
        created_at: '2026-07-31T00:00:00Z',
      },
    })

    expect(parsed.kind).toBe('file')
    expect(parsed.active_revision?.file_type).toBe('markdown')
    expect(parsed).not.toHaveProperty('raw_storage_key')
    expect(
      documentResponseSchema.safeParse({ ...parsed, kind: 'faq' }).success
    ).toBe(true)
    expect(
      documentResponseSchema.safeParse({
        ...parsed,
        kind: 'web',
        source_uri: 'https://example.com/page',
      }).success
    ).toBe(true)
  })

  it('rejects legacy mutable pipeline statuses', () => {
    const base = {
      id: '10000000-0000-4000-8000-000000000001',
      workspace_id: '20000000-0000-4000-8000-000000000002',
      knowledge_base_id: '30000000-0000-4000-8000-000000000003',
      kind: 'file',
      title: '指南.md',
      source_type: 'upload',
      source_uri: null,
      normalized_markdown: '',
      metadata: {},
      error_message: '',
      created_at: '2026-07-31T00:00:00Z',
      updated_at: '2026-07-31T00:00:00Z',
    }
    expect(
      documentResponseSchema.safeParse({ ...base, status: 'processing' })
        .success
    ).toBe(true)
    expect(
      documentResponseSchema.safeParse({ ...base, status: 'indexing' }).success
    ).toBe(false)
  })
})
