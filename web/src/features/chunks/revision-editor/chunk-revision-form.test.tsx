import { describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import type { Chunk, ChunkRevision } from '@/features/chunks/types'
import { ApiError } from '@/lib/api/error'
import { ChunkRevisionForm } from './chunk-revision-form'

const activeRevision: ChunkRevision = {
  id: '70000000-0000-4000-8000-000000000007',
  chunk_id: '30000000-0000-4000-8000-000000000003',
  revision_no: 3,
  content: '当前内容',
  context_header: '安装指南',
  enabled: true,
  status: 'ready',
  edit_source: 'user',
  editor_display_name: '林墨',
  error_message: '',
  created_at: '2026-08-01T10:00:00Z',
}

const chunk: Chunk = {
  id: activeRevision.chunk_id,
  workspace_id: '10000000-0000-4000-8000-000000000001',
  knowledge_base_id: '20000000-0000-4000-8000-000000000002',
  document_id: '40000000-0000-4000-8000-000000000004',
  document_revision_id: '50000000-0000-4000-8000-000000000005',
  chunk_set_id: '60000000-0000-4000-8000-000000000006',
  sequence: 1,
  source_content: '原始内容',
  source_anchor: {},
  metadata: {},
  active_revision: activeRevision,
  created_at: '2026-08-01T09:00:00Z',
}

describe('ChunkRevisionForm', () => {
  it('always submits the active base revision and validates enabled content', async () => {
    const saveRevision = vi.fn().mockResolvedValue(activeRevision)
    const screen = await render(
      <ChunkRevisionForm chunk={chunk} saveRevision={saveRevision} />
    )
    await userEvent.clear(screen.getByLabelText('内容'))
    await userEvent.click(screen.getByRole('button', { name: '保存新修订' }))
    await expect
      .element(screen.getByText('启用检索时内容不能为空'))
      .toBeVisible()
    expect(saveRevision).not.toHaveBeenCalled()

    await userEvent.fill(screen.getByLabelText('内容'), '新的内容')
    await userEvent.click(screen.getByRole('button', { name: '保存新修订' }))
    await vi.waitFor(() =>
      expect(saveRevision).toHaveBeenCalledWith({
        base_revision_id: activeRevision.id,
        content: '新的内容',
        context_header: '安装指南',
        enabled: true,
      })
    )
  })

  it('reports unsaved state so the parent surface can guard closing', async () => {
    const onDirtyChange = vi.fn()
    const screen = await render(
      <ChunkRevisionForm
        chunk={chunk}
        saveRevision={vi.fn().mockResolvedValue(activeRevision)}
        onDirtyChange={onDirtyChange}
      />
    )
    await userEvent.fill(screen.getByLabelText('内容'), '追加内容')
    await vi.waitFor(() => expect(onDirtyChange).toHaveBeenLastCalledWith(true))
  })

  it('preserves the draft and compares the latest revision after a conflict', async () => {
    const latest = {
      ...activeRevision,
      id: '80000000-0000-4000-8000-000000000008',
      revision_no: 4,
      content: '他人的新版本',
    }
    const saveRevision = vi
      .fn()
      .mockRejectedValueOnce(new ApiError('版本冲突', 409, 'revision_conflict'))
      .mockResolvedValueOnce(latest)
    const screen = await render(
      <ChunkRevisionForm
        chunk={chunk}
        latestRevision={latest}
        saveRevision={saveRevision}
      />
    )
    await userEvent.clear(screen.getByLabelText('内容'))
    await userEvent.fill(screen.getByLabelText('内容'), '我的未保存版本')
    await userEvent.click(screen.getByRole('button', { name: '保存新修订' }))

    await expect.element(screen.getByText('分块已被他人更新')).toBeVisible()
    await expect.element(screen.getByText('你的版本')).toBeVisible()
    await expect
      .element(screen.getByRole('heading', { name: '最新版本' }))
      .toBeVisible()
    await expect.element(screen.getByText('他人的新版本')).toBeVisible()
    await expect
      .element(screen.getByLabelText('内容'))
      .toHaveValue('我的未保存版本')
    await userEvent.click(
      screen.getByRole('button', { name: '基于最新版本重试' })
    )
    await vi.waitFor(() =>
      expect(saveRevision).toHaveBeenLastCalledWith({
        base_revision_id: latest.id,
        content: '我的未保存版本',
        context_header: '安装指南',
        enabled: true,
      })
    )
  })
})
