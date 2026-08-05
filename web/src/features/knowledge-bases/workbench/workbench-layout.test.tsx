import { describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import type { KnowledgeBase } from '@/features/knowledge-bases/types'
import type { KnowledgeBaseSummary } from './types'
import { KnowledgeBaseWorkbenchLayout } from './workbench-layout'

vi.mock('@tanstack/react-router', () => ({
  Link: ({
    children,
    to,
    ...props
  }: React.ComponentProps<'a'> & { to: string }) => (
    <a href={to} {...props}>
      {children}
    </a>
  ),
  Outlet: () => <div>叶子路由内容</div>,
  useLocation: () => ({
    pathname: '/workspaces/acme/kb/de305d54-75b4-431b-adb2-eb6b9e546014',
  }),
  useNavigate: () => vi.fn(),
  useMatches: () => [],
}))

const kbId = 'de305d54-75b4-431b-adb2-eb6b9e546014'
const workspaceId = 'f064b7d4-eba3-4d1d-8b54-b666e83d63e5'
const generationId = '5de1f306-118b-4c2e-86f8-acde3cb6bdb4'

const knowledgeBase: KnowledgeBase = {
  id: kbId,
  workspace_id: workspaceId,
  name: '产品文档',
  description: '面向产品、交付与故障排查的内部知识',
  embedding_model_id: 'f52bec15-7278-457e-8ad6-c545bfb07c57',
  embedding_model: {
    id: 'f52bec15-7278-457e-8ad6-c545bfb07c57',
    name: 'text-embedding-3-large',
    display_name: 'Text Embedding 3 Large',
    provider: 'openai',
    provider_display_name: 'OpenAI Compatible',
    dimensions: 3584,
    available: true,
  },
  chunking_config: { chunk_size: 1000, chunk_overlap: 100 },
  retrieval_config: {
    fts_config: 'simple',
    vector_top_k: 20,
    keyword_top_k: 20,
    final_top_k: 8,
    rrf_k: 60,
  },
  content_version: 18,
  active_index_generation_id: generationId,
  file_tree_root_id: 'c373dee3-3cfc-42b6-985c-f9b46807b97d',
  metadata: { config_hash: 'must-not-render' },
  created_at: '2026-08-01T09:42:00Z',
  updated_at: '2026-08-01T10:00:00Z',
}

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
    retrieval_config: { final_top_k: 8 },
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
  candidate_generation: null,
  sync_state: 'failed',
  recent_jobs: [],
  blockers: [],
}

describe('KnowledgeBaseWorkbenchLayout', () => {
  it('keeps readable KB context around a nested Outlet and route navigation', async () => {
    const screen = await render(
      <KnowledgeBaseWorkbenchLayout
        workspaceSlug='acme'
        kbId={kbId}
        knowledgeBase={knowledgeBase}
        summary={summary}
      />
    )

    await expect
      .element(screen.getByText('产品文档', { exact: true }))
      .toBeVisible()
    await expect.element(screen.getByText('有失败')).toBeVisible()
    for (const item of ['概览', '内容', '检索测试', '索引', '设置']) {
      await expect
        .element(screen.getByRole('link', { name: item, exact: true }))
        .toBeVisible()
    }
    await expect
      .element(screen.getByRole('combobox', { name: '知识库区域' }))
      .toBeInTheDocument()
    await expect.element(screen.getByText('叶子路由内容')).toBeVisible()
    expect(document.body.textContent).not.toContain(kbId)
    expect(document.body.textContent).not.toContain(workspaceId)
    expect(document.body.textContent).not.toContain('must-not-render')
  })
})
