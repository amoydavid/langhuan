import { describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
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
  it('groups children under their read-only parent', async () => {
    const parentId = '30000000-0000-4000-8000-000000000009'
    const parent: Chunk = {
      ...chunk,
      id: parentId,
      role: 'parent',
      parent_chunk_id: null,
      sequence: 0,
      active_revision: {
        ...chunk.active_revision!,
        chunk_id: parentId,
        content: '完整上下文',
      },
    }
    const child: Chunk = { ...chunk, role: 'child', parent_chunk_id: parentId }
    const screen = await render(
      <ChunkInspector
        documentTitle='installation.md'
        documentKind='file'
        chunks={[parent, child]}
        page={1}
        canEdit
      />
    )

    await expect
      .element(screen.getByText('上下文块 1 · 1 个子块'))
      .toBeVisible()
    await expect
      .element(screen.getByRole('button', { name: '编辑分块 0' }))
      .not.toBeInTheDocument()
    await expect
      .element(screen.getByRole('button', { name: '编辑分块 1' }))
      .toBeVisible()
  })

  it('renders chunk cards with source anchor and revision preview', async () => {
    const onSelectChunk = vi.fn()
    const screen = await render(
      <ChunkInspector
        documentTitle='installation.md'
        documentKind='file'
        chunks={[chunk]}
        selectedChunkId={chunkId}
        page={1}
        canEdit={false}
        onSelectChunk={onSelectChunk}
      />
    )

    // context header + 来源锚点 + 内容预览 都在卡片上可见
    await expect
      .element(screen.getByText('安装指南 > Docker 部署', { exact: true }))
      .toBeVisible()
    await expect.element(screen.getByText('第 24–31 行')).toBeVisible()
    await expect
      .element(screen.getByText('通过 DATABASE_DSN 指定 PostgreSQL。'))
      .toBeVisible()
    // 无权限时编辑按钮不渲染
    await expect
      .element(screen.getByRole('button', { name: '编辑分块 1' }))
      .not.toBeInTheDocument()
    // id / 内部 hash 不应泄漏到 DOM 文本中
    expect(document.body.textContent).not.toContain(chunkId)
    expect(document.body.textContent).not.toContain('must-not-render')

    // 点击卡片触发查看
    await userEvent.click(screen.getByRole('button', { name: '查看分块 1' }))
    expect(onSelectChunk).toHaveBeenCalledWith(chunkId)
  })

  it('gates the per-card edit button behind canEdit and document kind', async () => {
    const onEdit = vi.fn()

    // FAQ 文档即便有权限也不显示编辑
    const faqScreen = await render(
      <ChunkInspector
        documentTitle='退款政策'
        documentKind='faq'
        chunks={[chunk]}
        page={1}
        canEdit
        onEdit={onEdit}
      />
    )
    await expect.element(faqScreen.getByText('由 FAQ 内容生成')).toBeVisible()
    await expect
      .element(faqScreen.getByRole('button', { name: '编辑分块 1' }))
      .not.toBeInTheDocument()

    // 普通文件 + 有权限才显示编辑
    const fileScreen = await render(
      <ChunkInspector
        documentTitle='installation.md'
        documentKind='file'
        chunks={[chunk]}
        page={1}
        canEdit
        onEdit={onEdit}
      />
    )
    const editBtn = fileScreen.getByRole('button', { name: '编辑分块 1' })
    await expect.element(editBtn).toBeVisible()
    await userEvent.click(editBtn)
    expect(onEdit).toHaveBeenCalledWith(chunk)
  })
})
