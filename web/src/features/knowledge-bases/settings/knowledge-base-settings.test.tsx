import { describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import type { IndexGeneration } from '@/features/index-generations/types'
import type { KnowledgeBase } from '@/features/knowledge-bases/types'
import { KnowledgeBaseSettings } from './knowledge-base-settings'

const kbId = '20000000-0000-4000-8000-000000000002'
const generationId = '30000000-0000-4000-8000-000000000003'

const knowledgeBase: KnowledgeBase = {
  id: kbId,
  workspace_id: '10000000-0000-4000-8000-000000000001',
  name: '产品文档',
  description: '面向产品与交付',
  embedding_model_id: '40000000-0000-4000-8000-000000000004',
  embedding_model: {
    id: '40000000-0000-4000-8000-000000000004',
    name: 'embedding-large',
    display_name: 'OpenAI Embedding Large',
    provider: 'openai',
    provider_display_name: 'OpenAI',
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
  file_tree_root_id: '50000000-0000-4000-8000-000000000005',
  metadata: { config_hash: 'do-not-render' },
  created_at: '2026-08-01T09:00:00Z',
  updated_at: '2026-08-01T10:00:00Z',
}

const activeGeneration = {
  id: generationId,
  workspace_id: knowledgeBase.workspace_id,
  knowledge_base_id: kbId,
  embedding_model_id: knowledgeBase.embedding_model_id,
  provider_id: '60000000-0000-4000-8000-000000000006',
  model_name: 'text-embedding-3-large',
  display_label: '2026-08-01 09:42 · text-embedding-3-large · 已就绪',
  embedding_dimension: 3584,
  chunker_version: 1,
  chunking_config: { chunk_size: 1000, chunk_overlap: 100 },
  retrieval_config: knowledgeBase.retrieval_config,
  config_hash: 'generation-config-hash',
  source_content_version: 18,
  indexed_content_version: 18,
  status: 'ready',
  document_count: 20,
  chunk_count: 38,
  indexed_count: 38,
  manual_edit_count: 0,
  disabled_chunk_count: 0,
  manual_edit_disposition: 'not_applicable',
  error_class: '',
  error_message: '',
  created_at: '2026-08-01T09:42:00Z',
} satisfies IndexGeneration

describe('KnowledgeBaseSettings', () => {
  it('allows admin to patch only readable basics and copies diagnostics explicitly', async () => {
    const saveBasics = vi.fn().mockResolvedValue({
      ...knowledgeBase,
      name: '交付知识',
    })
    const copyText = vi.fn().mockResolvedValue(undefined)
    const screen = await render(
      <KnowledgeBaseSettings
        knowledgeBase={knowledgeBase}
        activeGeneration={activeGeneration}
        canManage
        saveBasics={saveBasics}
        copyText={copyText}
        buildIndexHref='/indexes?create=true'
      />
    )

    await userEvent.clear(screen.getByLabelText('知识库名称'))
    await userEvent.fill(screen.getByLabelText('知识库名称'), '交付知识')
    await userEvent.click(screen.getByRole('button', { name: '保存基本信息' }))
    expect(saveBasics).toHaveBeenCalledWith({
      name: '交付知识',
      description: '面向产品与交付',
    })
    expect(document.body.textContent).not.toContain(kbId)
    expect(document.body.textContent).not.toContain(generationId)
    expect(document.body.textContent).not.toContain('generation-config-hash')

    await userEvent.click(screen.getByRole('button', { name: '复制诊断信息' }))
    expect(copyText).toHaveBeenCalledWith(expect.stringContaining(kbId))
    expect(copyText).toHaveBeenCalledWith(expect.stringContaining(generationId))
  })

  it('keeps member view read-only with an explicit permission explanation', async () => {
    const screen = await render(
      <KnowledgeBaseSettings
        knowledgeBase={knowledgeBase}
        activeGeneration={activeGeneration}
        canManage={false}
        saveBasics={vi.fn()}
        copyText={vi.fn()}
        buildIndexHref='/indexes?create=true'
      />
    )

    await expect.element(screen.getByLabelText('知识库名称')).toBeDisabled()
    await expect
      .element(screen.getByText('知识库基本信息由 Workspace 管理员维护'))
      .toBeVisible()
    await expect
      .element(screen.getByRole('button', { name: '保存基本信息' }))
      .not.toBeInTheDocument()
  })
})
