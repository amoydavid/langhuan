import { describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import type { Chunk } from '@/features/chunks/types'
import { ChunkInspector } from './chunk-inspector'

const chunkId = '30000000-0000-4000-8000-000000000003'

const chunk: Chunk = {
  id: chunkId,
  workspace_id: '10000000-0000-4000-8000-000000000001',
  knowledge_base_id: '20000000-0000-4000-8000-000000000002',
  document_id: '40000000-0000-4000-8000-000000000004',
  document_revision_id: '50000000-0000-4000-8000-000000000005',
  chunk_set_id: '60000000-0000-4000-8000-000000000006',
  sequence: 1,
  source_content: 'Docker 部署时，通过 DATABASE_DSN 指定 PostgreSQL。',
  source_anchor: { line_start: 24, line_end: 31 },
  metadata: { heading: 'Docker 部署', internal_hash: 'must-not-render' },
  active_revision: {
    id: '70000000-0000-4000-8000-000000000007',
    chunk_id: chunkId,
    revision_no: 3,
    content: '通过 DATABASE_DSN 指定 PostgreSQL。',
    context_header: '安装指南 > Docker 部署',
    enabled: true,
    status: 'ready',
    edit_source: 'user',
    editor_display_name: '林墨',
    error_message: '',
    created_at: '2026-08-01T10:00:00Z',
  },
  created_at: '2026-08-01T09:00:00Z',
}

describe('ChunkInspector', () => {
  it('focuses a deep-linked chunk and shows readable source/current revision fields', async () => {
    const screen = await render(
      <ChunkInspector
        documentTitle='installation.md'
        documentKind='file'
        chunks={[chunk]}
        revisions={chunk.active_revision ? [chunk.active_revision] : []}
        selectedChunkId={chunkId}
        canEdit={false}
      />
    )

    const heading = screen.getByRole('heading', {
      name: '分块 1 · installation.md',
    })
    await expect.element(heading).toHaveFocus()
    await expect.element(screen.getByText('第 24–31 行')).toBeVisible()
    await expect
      .element(screen.getByText('安装指南 > Docker 部署', { exact: true }))
      .toBeVisible()
    await expect.element(screen.getByText('林墨')).toBeVisible()
    await expect
      .element(screen.getByRole('tab', { name: '修订历史' }))
      .toBeVisible()
    await expect
      .element(screen.getByRole('button', { name: '编辑分块' }))
      .not.toBeInTheDocument()
    expect(document.body.textContent).not.toContain(chunkId)
    expect(document.body.textContent).not.toContain('must-not-render')
  })

  it('keeps FAQ chunks immutable and allows administrators to edit File chunks', async () => {
    const onEdit = vi.fn()
    const screen = await render(
      <ChunkInspector
        documentTitle='退款政策'
        documentKind='faq'
        chunks={[chunk]}
        selectedChunkId={chunkId}
        canEdit
        onEdit={onEdit}
      />
    )
    await expect.element(screen.getByText('由 FAQ 内容生成')).toBeVisible()
    await expect
      .element(screen.getByRole('button', { name: '编辑分块' }))
      .not.toBeInTheDocument()
  })
})
