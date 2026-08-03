import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-react'
import type { KnowledgeBaseSummary } from '@/features/knowledge-bases/workbench/types'
import { KnowledgeBaseOverview } from './knowledge-base-overview'

const kbId = 'de305d54-75b4-431b-adb2-eb6b9e546014'
const generationId = '5de1f306-118b-4c2e-86f8-acde3cb6bdb4'
const jobId = '184d3f72-7840-4b35-a943-3d5c68a9064f'

const summary: KnowledgeBaseSummary = {
  knowledge_base_id: kbId,
  knowledge_base_name: '产品文档',
  content_version: 18,
  document_counts: {
    total: 20,
    file: 14,
    faq: 5,
    web: 1,
    ready: 18,
    processing: 1,
    failed: 1,
  },
  active_generation: {
    id: generationId,
    display_label: '2026-08-01 09:42 · Text Embedding 3 Large · 当前生效',
    status: 'ready',
    model_display_name: 'Text Embedding 3 Large',
    embedding_dimension: 3584,
    chunker_version: 1,
    chunking_config: { chunk_size: 1000, chunk_overlap: 100 },
    retrieval_config: { vector_top_k: 20, keyword_top_k: 20, final_top_k: 8 },
    source_content_version: 18,
    indexed_content_version: 18,
    document_count: 20,
    chunk_count: 38,
    indexed_count: 38,
    manual_edit_count: 2,
    disabled_chunk_count: 1,
    error_message: '',
    created_at: '2026-08-01T09:42:00Z',
  },
  candidate_generation: {
    id: 'a4d03b5d-16ff-46a3-8189-5a27c48262e8',
    display_label: '2026-08-01 11:08 · Text Embedding V4 · 待激活',
    status: 'ready',
    model_display_name: 'Text Embedding V4',
    embedding_dimension: 1024,
    chunker_version: 1,
    chunking_config: { chunk_size: 800, chunk_overlap: 80 },
    retrieval_config: { final_top_k: 8 },
    source_content_version: 18,
    indexed_content_version: 18,
    document_count: 20,
    chunk_count: 44,
    indexed_count: 44,
    manual_edit_count: 2,
    disabled_chunk_count: 1,
    error_message: '',
    created_at: '2026-08-01T11:08:00Z',
  },
  sync_state: 'candidate_ready',
  recent_jobs: [
    {
      id: jobId,
      status: 'running',
      action_label: '导入文件',
      target_type: 'document',
      target_display_name: 'installation.md',
      attempts: 1,
      error_message: '',
      created_at: '2026-08-01T10:00:00Z',
      updated_at: '2026-08-01T10:01:00Z',
    },
  ],
  blockers: [
    {
      code: 'document_processing_failed',
      resource_type: 'document',
      resource_id: '087a124b-859f-4786-902a-2dd1901a006f',
      resource_display_name: 'faq-import.csv',
      message: '内容处理失败，请查看任务并重新导入或删除内容。',
    },
  ],
}

describe('KnowledgeBaseOverview', () => {
  it('renders readable health, active index, blockers and jobs for a member', async () => {
    const screen = await render(
      <KnowledgeBaseOverview
        workspaceSlug='acme'
        kbId={kbId}
        summary={summary}
        canManageIndex={false}
      />
    )

    await expect.element(screen.getByText('全部内容')).toBeVisible()
    await expect.element(screen.getByText('20', { exact: true })).toBeVisible()
    await expect.element(screen.getByText('18', { exact: true })).toBeVisible()
    await expect
      .element(screen.getByText(summary.active_generation?.display_label ?? ''))
      .toBeVisible()
    await expect.element(screen.getByText('faq-import.csv')).toBeVisible()
    await expect.element(screen.getByText('installation.md')).toBeVisible()
    await expect
      .element(screen.getByText('构建新索引版本', { exact: true }))
      .not.toBeInTheDocument()
    expect(document.body.textContent).not.toContain(kbId)
    expect(document.body.textContent).not.toContain(generationId)
    expect(document.body.textContent).not.toContain(jobId)
  })

  it('exposes index construction only to an administrator', async () => {
    const screen = await render(
      <KnowledgeBaseOverview
        workspaceSlug='acme'
        kbId={kbId}
        summary={summary}
        canManageIndex
      />
    )

    await expect
      .element(screen.getByText('构建新索引版本', { exact: true }))
      .toBeVisible()
  })

  it('labels the current content and indexed versions without exposing the build baseline', async () => {
    const currentSummary: KnowledgeBaseSummary = {
      ...summary,
      content_version: 1,
      active_generation: summary.active_generation
        ? {
            ...summary.active_generation,
            source_content_version: 0,
            indexed_content_version: 1,
          }
        : null,
    }
    const screen = await render(
      <KnowledgeBaseOverview
        workspaceSlug='acme'
        kbId={kbId}
        summary={currentSummary}
        canManageIndex={false}
      />
    )

    await expect.element(screen.getByText('内容版本 1')).toBeVisible()
    await expect.element(screen.getByText('已索引 1')).toBeVisible()
    expect(document.body.textContent).not.toContain('1 / 0')
  })
})
