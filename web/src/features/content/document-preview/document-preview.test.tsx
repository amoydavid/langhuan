import { describe, expect, it } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import type { Document } from '@/features/documents/types'
import { DocumentPreview } from './document-preview'

const documentId = '30000000-0000-4000-8000-000000000003'
const revisionId = '40000000-0000-4000-8000-000000000004'

const item: Document = {
  id: documentId,
  workspace_id: '10000000-0000-4000-8000-000000000001',
  knowledge_base_id: '20000000-0000-4000-8000-000000000002',
  kind: 'file',
  title: '旧标题',
  source_type: 'upload',
  source_uri: null,
  status: 'ready',
  normalized_markdown:
    '# 安装指南\n\n<script>alert(1)</script>\n\n准备 PostgreSQL。',
  metadata: { provider_secret: 'must-not-render', sheet: 'Sheet1' },
  error_message: '',
  created_at: '2026-08-01T09:00:00Z',
  updated_at: '2026-08-01T10:00:00Z',
  active_revision: {
    id: revisionId,
    revision_no: 2,
    status: 'ready',
    original_filename: 'installation.md',
    file_type: 'markdown',
    content_type: 'text/markdown',
    sha256: 'must-not-render-hash',
    size_bytes: 2048,
    created_at: '2026-08-01T09:00:00Z',
  },
}

describe('DocumentPreview', () => {
  it('uses the File Tree name/path and renders sanitized Markdown by default', async () => {
    const screen = await render(
      <DocumentPreview
        document={item}
        displayName='installation.md'
        path='docs/guides/installation.md'
      />
    )

    await expect
      .element(screen.getByRole('heading', { name: 'installation.md' }))
      .toBeVisible()
    await expect
      .element(screen.getByText('docs/guides/installation.md'))
      .toBeVisible()
    await expect
      .element(screen.getByRole('heading', { name: '安装指南' }))
      .toBeVisible()
    const preview = document.querySelector('[data-slot="tabs-content"]')
    expect(preview?.querySelector('script')).toBeNull()
    expect(document.body.textContent).not.toContain(documentId)
    expect(document.body.textContent).not.toContain(revisionId)
    expect(document.body.textContent).not.toContain('must-not-render')
  })

  it('switches to raw Markdown and exposes only typed file information', async () => {
    const screen = await render(
      <DocumentPreview
        document={item}
        displayName='installation.md'
        path='docs/guides/installation.md'
      />
    )
    await userEvent.click(screen.getByRole('tab', { name: '原始 Markdown' }))
    await expect
      .element(screen.getByText('# 安装指南', { exact: false }))
      .toBeVisible()
    await userEvent.click(screen.getByRole('tab', { name: '文件信息' }))
    await expect.element(screen.getByText('text/markdown')).toBeVisible()
    await expect.element(screen.getByText('2.0 KB')).toBeVisible()
    await expect.element(screen.getByText('第 2 版')).toBeVisible()
  })
})
