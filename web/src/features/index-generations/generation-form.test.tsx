import { describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import { GenerationForm } from './generation-form'
import type { IndexGeneration } from './types'

const baseGeneration = {
  id: '30000000-0000-4000-8000-000000000003',
  workspace_id: '10000000-0000-4000-8000-000000000001',
  knowledge_base_id: '20000000-0000-4000-8000-000000000002',
  embedding_model_id: '40000000-0000-4000-8000-000000000004',
  provider_id: '50000000-0000-4000-8000-000000000005',
  model_name: 'text-embedding-3-large',
  display_label: '2026-08-01 09:42 · text-embedding-3-large · 已就绪',
  embedding_dimension: 3584,
  chunker_version: 3,
  chunking_config: {
    strategy: 'auto',
    enable_parent_child: true,
    parent_chunk_size: 4096,
    child_chunk_size: 384,
    chunk_size: 1000,
    chunk_overlap: 100,
  },
  retrieval_config: {
    fts_config: 'simple',
    vector_top_k: 20,
    keyword_top_k: 20,
    final_top_k: 8,
    rrf_k: 60,
  },
  config_hash: 'hidden',
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

describe('GenerationForm', () => {
  it('uses the active generation as defaults and submits the complete three-step config', async () => {
    const createGeneration = vi.fn().mockResolvedValue(baseGeneration)
    const screen = await render(
      <GenerationForm
        models={[
          {
            id: baseGeneration.embedding_model_id,
            displayName: 'OpenAI Embedding Large',
            dimensions: 3584,
          },
        ]}
        baseGeneration={baseGeneration}
        createGeneration={createGeneration}
      />
    )

    await expect.element(screen.getByText('1. Embedding 模型')).toBeVisible()
    await userEvent.click(
      screen.getByRole('button', { name: '下一步：分块配置' })
    )
    await expect
      .element(screen.getByLabelText('父子分块', { exact: true }))
      .toBeChecked()
    await expect
      .element(screen.getByLabelText('小块大小（用于召回）', { exact: true }))
      .toHaveValue(384)
    await userEvent.click(
      screen.getByRole('button', { name: '下一步：检索配置' })
    )
    await expect
      .element(screen.getByLabelText('最终结果数', { exact: true }))
      .toHaveValue(8)
    await userEvent.click(screen.getByRole('button', { name: '构建索引版本' }))

    expect(createGeneration).toHaveBeenCalledWith({
      embedding_model_id: baseGeneration.embedding_model_id,
      chunking_config: {
        strategy: 'auto',
        enable_parent_child: true,
        parent_chunk_size: 4096,
        child_chunk_size: 384,
        chunk_size: 1000,
        chunk_overlap: 100,
      },
      retrieval_config: {
        fts_config: 'simple',
        vector_top_k: 20,
        keyword_top_k: 20,
        final_top_k: 8,
        rrf_k: 60,
      },
      rerank: { enabled: false },
    })
  })

  it('keeps parent-child draft values when switching to flat chunks and back', async () => {
    const createGeneration = vi.fn().mockResolvedValue(baseGeneration)
    const screen = await render(
      <GenerationForm
        models={[
          {
            id: baseGeneration.embedding_model_id,
            displayName: 'OpenAI Embedding Large',
            dimensions: 3584,
          },
        ]}
        baseGeneration={baseGeneration}
        createGeneration={createGeneration}
      />
    )

    await userEvent.click(
      screen.getByRole('button', { name: '下一步：分块配置' })
    )
    await userEvent.fill(screen.getByLabelText('小块大小（用于召回）'), '256')
    await userEvent.click(screen.getByLabelText('父子分块'))
    await expect
      .element(screen.getByLabelText('分块大小', { exact: true }))
      .toBeVisible()
    await userEvent.click(screen.getByLabelText('父子分块'))
    await expect
      .element(screen.getByLabelText('小块大小（用于召回）'))
      .toHaveValue(256)
  })

  it('picks the full-text search configuration from a select and shows hint tooltips', async () => {
    const createGeneration = vi.fn().mockResolvedValue(baseGeneration)
    const screen = await render(
      <GenerationForm
        models={[
          {
            id: baseGeneration.embedding_model_id,
            displayName: 'OpenAI Embedding Large',
            dimensions: 3584,
          },
        ]}
        baseGeneration={baseGeneration}
        createGeneration={createGeneration}
      />
    )

    await userEvent.click(
      screen.getByRole('button', { name: '下一步：分块配置' })
    )
    await expect
      .element(screen.getByLabelText('小块大小（用于召回）'))
      .toHaveValue(384)

    await userEvent.click(
      screen.getByRole('button', { name: '下一步：检索配置' })
    )
    // 全文检索配置是 Select 而非输入框，回填当前 generation 的 fts_config
    await expect
      .element(screen.getByRole('combobox', { name: '全文检索配置' }))
      .toHaveTextContent('通用（simple）')

    await userEvent.click(
      screen.getByRole('combobox', { name: '全文检索配置' })
    )
    await userEvent.click(
      screen.getByRole('option', { name: '中文（zhparser）' })
    )
    await userEvent.click(screen.getByRole('button', { name: '构建索引版本' }))

    expect(createGeneration).toHaveBeenCalledWith(
      expect.objectContaining({
        retrieval_config: expect.objectContaining({ fts_config: 'zhparser' }),
      })
    )
  })

  it('preserves a custom PostgreSQL full-text search configuration', async () => {
    const customBaseGeneration: IndexGeneration = {
      ...baseGeneration,
      retrieval_config: {
        ...baseGeneration.retrieval_config,
        fts_config: 'german',
      },
    }
    const createGeneration = vi.fn().mockResolvedValue(customBaseGeneration)
    const screen = await render(
      <GenerationForm
        models={[
          {
            id: customBaseGeneration.embedding_model_id,
            displayName: 'OpenAI Embedding Large',
            dimensions: 3584,
          },
        ]}
        baseGeneration={customBaseGeneration}
        createGeneration={createGeneration}
      />
    )

    await userEvent.click(
      screen.getByRole('button', { name: '下一步：分块配置' })
    )
    await userEvent.click(
      screen.getByRole('button', { name: '下一步：检索配置' })
    )
    await expect
      .element(screen.getByRole('combobox', { name: '全文检索配置' }))
      .toHaveTextContent('自定义')
    await expect
      .element(screen.getByLabelText('自定义全文检索配置'))
      .toHaveValue('german')

    await userEvent.click(screen.getByRole('button', { name: '构建索引版本' }))

    expect(createGeneration).toHaveBeenCalledWith(
      expect.objectContaining({
        retrieval_config: expect.objectContaining({ fts_config: 'german' }),
      })
    )
  })

  it('switches a preset full-text search configuration to a custom name', async () => {
    const createGeneration = vi.fn().mockResolvedValue(baseGeneration)
    const screen = await render(
      <GenerationForm
        models={[
          {
            id: baseGeneration.embedding_model_id,
            displayName: 'OpenAI Embedding Large',
            dimensions: 3584,
          },
        ]}
        baseGeneration={baseGeneration}
        createGeneration={createGeneration}
      />
    )

    await userEvent.click(
      screen.getByRole('button', { name: '下一步：分块配置' })
    )
    await userEvent.click(
      screen.getByRole('button', { name: '下一步：检索配置' })
    )
    await userEvent.click(
      screen.getByRole('combobox', { name: '全文检索配置' })
    )
    await userEvent.click(screen.getByRole('option', { name: '自定义' }))
    await userEvent.fill(screen.getByLabelText('自定义全文检索配置'), 'german')
    await userEvent.click(screen.getByRole('button', { name: '构建索引版本' }))

    expect(createGeneration).toHaveBeenCalledWith(
      expect.objectContaining({
        retrieval_config: expect.objectContaining({ fts_config: 'german' }),
      })
    )
  })
})
