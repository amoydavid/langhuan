import { describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import type { Chunk, ChunkRevision } from '@/features/chunks/types'
import { ChunkDetailDialog } from './chunk-detail-dialog'

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

const olderRevision: ChunkRevision = {
  ...chunk.active_revision!,
  id: '70000000-0000-4000-8000-000000000008',
  revision_no: 2,
  edit_source: 'system',
  editor_display_name: '系统',
}

describe('ChunkDetailDialog', () => {
  it('stays closed and renders nothing when no chunk is provided', async () => {
    const onOpenChange = vi.fn()
    await render(
      <ChunkDetailDialog
        chunk={undefined}
        documentTitle='installation.md'
        documentKind='file'
        canEdit
        onOpenChange={onOpenChange}
      />
    )

    // 关闭时不渲染 dialog 标题与正文内容
    expect(document.body.textContent).not.toContain('installation.md')
    expect(document.body.textContent).not.toContain('林墨')
    expect(document.body.textContent).not.toContain('编辑分块')
  })

  it('renders current content / source / history tabs with readable fields', async () => {
    const screen = await render(
      <ChunkDetailDialog
        chunk={chunk}
        documentTitle='installation.md'
        documentKind='file'
        revisions={[chunk.active_revision!, olderRevision]}
        canEdit={false}
        onOpenChange={vi.fn()}
      />
    )

    // 标题带文档名 + 锚点描述
    await expect
      .element(
        screen.getByRole('heading', { name: '分块 1 · installation.md' })
      )
      .toBeVisible()
    await expect.element(screen.getByText('第 24–31 行')).toBeVisible()

    // 当前内容：context header + 编辑者可见
    await expect
      .element(screen.getByText('安装指南 > Docker 部署', { exact: true }))
      .toBeVisible()
    await expect.element(screen.getByText('林墨')).toBeVisible()

    // 原始来源 tab
    await userEvent.click(screen.getByRole('tab', { name: '原始来源' }))
    await expect
      .element(
        screen.getByText('Docker 部署时，通过 DATABASE_DSN 指定 PostgreSQL。')
      )
      .toBeVisible()

    // 修订历史 tab：按修订号倒序，最新在前
    await userEvent.click(screen.getByRole('tab', { name: '修订历史' }))
    await expect
      .element(screen.getByText('修订 3', { exact: true }))
      .toBeVisible()
    await expect
      .element(screen.getByText('修订 2', { exact: true }))
      .toBeVisible()

    // 无权限时编辑按钮不渲染
    await expect
      .element(screen.getByRole('button', { name: '编辑分块' }))
      .not.toBeInTheDocument()

    // id / 内部 hash 不泄漏
    expect(document.body.textContent).not.toContain(chunkId)
    expect(document.body.textContent).not.toContain('must-not-render')
  })

  it('gates the edit button behind canEdit and document kind, and fires onEdit', async () => {
    const onEdit = vi.fn()

    // FAQ 文档即便有权限也不显示编辑
    const faqScreen = await render(
      <ChunkDetailDialog
        chunk={chunk}
        documentTitle='退款政策'
        documentKind='faq'
        canEdit
        onOpenChange={vi.fn()}
        onEdit={onEdit}
      />
    )
    await expect
      .element(faqScreen.getByRole('button', { name: '编辑分块' }))
      .not.toBeInTheDocument()

    // 普通文件 + 有权限才显示，点击触发 onEdit
    const fileScreen = await render(
      <ChunkDetailDialog
        chunk={chunk}
        documentTitle='installation.md'
        documentKind='file'
        canEdit
        onOpenChange={vi.fn()}
        onEdit={onEdit}
      />
    )
    const editBtn = fileScreen.getByRole('button', { name: '编辑分块' })
    await expect.element(editBtn).toBeVisible()
    await userEvent.click(editBtn)
    expect(onEdit).toHaveBeenCalledWith(chunk)
  })

  it('renders the empty history hint when no revisions are provided', async () => {
    const screen = await render(
      <ChunkDetailDialog
        chunk={chunk}
        documentTitle='installation.md'
        documentKind='file'
        revisions={[]}
        canEdit={false}
        onOpenChange={vi.fn()}
      />
    )
    await userEvent.click(screen.getByRole('tab', { name: '修订历史' }))
    await expect.element(screen.getByText('暂无修订历史。')).toBeVisible()
  })
})
