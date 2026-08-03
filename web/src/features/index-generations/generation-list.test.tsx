import { describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import { GenerationList } from './generation-list'
import type { IndexGeneration } from './types'

const activeId = '30000000-0000-4000-8000-000000000003'
const candidateId = '40000000-0000-4000-8000-000000000004'

function generation(
  id: string,
  status: IndexGeneration['status'],
  overrides: Partial<IndexGeneration> = {}
): IndexGeneration {
  return {
    id,
    workspace_id: '10000000-0000-4000-8000-000000000001',
    knowledge_base_id: '20000000-0000-4000-8000-000000000002',
    embedding_model_id: '50000000-0000-4000-8000-000000000005',
    provider_id: '60000000-0000-4000-8000-000000000006',
    model_name: 'text-embedding-v4',
    display_label: `2026-08-01 11:08 · text-embedding-v4 · ${status}`,
    embedding_dimension: 1024,
    chunker_version: 1,
    chunking_config: { chunk_size: 1000, chunk_overlap: 100 },
    retrieval_config: {
      fts_config: 'simple',
      vector_top_k: 20,
      keyword_top_k: 20,
      final_top_k: 8,
      rrf_k: 60,
    },
    config_hash: 'do-not-render-config-hash',
    source_content_version: 18,
    indexed_content_version: 18,
    status,
    document_count: 20,
    chunk_count: 44,
    indexed_count: 44,
    manual_edit_count: 2,
    disabled_chunk_count: 1,
    manual_edit_disposition: 'not_applicable',
    error_class: '',
    error_message: '',
    created_at: '2026-08-01T11:08:00Z',
    ...overrides,
  }
}

describe('GenerationList', () => {
  it('requires explicit archival confirmation before activating a ready candidate', async () => {
    const activateGeneration = vi.fn().mockResolvedValue(undefined)
    const candidate = generation(candidateId, 'ready', {
      manual_edit_disposition: 'pending',
      manual_edit_count: 2,
    })
    const screen = await render(
      <GenerationList
        generations={[candidate, generation(activeId, 'ready')]}
        activeGenerationId={activeId}
        canManage
        activateGeneration={activateGeneration}
      />
    )

    await userEvent.click(screen.getByRole('button', { name: '比较并激活' }))
    await expect.element(screen.getByText('将归档 2 条人工修订')).toBeVisible()
    await expect
      .element(screen.getByRole('button', { name: '确认激活' }))
      .toBeDisabled()
    await userEvent.click(screen.getByLabelText('确认归档人工修订'))
    await userEvent.click(screen.getByRole('button', { name: '确认激活' }))
    expect(activateGeneration).toHaveBeenCalledWith(candidate, true)
  })

  it('shows readable states and keeps member actions read-only without IDs or hashes', async () => {
    const screen = await render(
      <GenerationList
        generations={[
          generation(activeId, 'ready'),
          generation(candidateId, 'building'),
        ]}
        activeGenerationId={activeId}
        canManage={false}
        activateGeneration={vi.fn()}
      />
    )

    await expect.element(screen.getByText('当前生效')).toBeVisible()
    await expect.element(screen.getByText('构建中')).toBeVisible()
    await expect
      .element(screen.getByText('索引配置由 Workspace 管理员维护'))
      .toBeVisible()
    await expect
      .element(screen.getByRole('button', { name: '比较并激活' }))
      .not.toBeInTheDocument()
    expect(document.body.textContent).not.toContain(activeId)
    expect(document.body.textContent).not.toContain(candidateId)
    expect(document.body.textContent).not.toContain('do-not-render-config-hash')
  })

  it('distinguishes the active content version from candidate build snapshots', async () => {
    const screen = await render(
      <GenerationList
        generations={[
          generation(candidateId, 'ready'),
          generation(activeId, 'ready', {
            source_content_version: 0,
            indexed_content_version: 1,
          }),
        ]}
        activeGenerationId={activeId}
        currentContentVersion={1}
        canManage={false}
        activateGeneration={vi.fn()}
      />
    )

    await expect
      .element(screen.getByText('内容版本 1 · 已索引 1'))
      .toBeVisible()
    await expect
      .element(screen.getByText('内容快照 18 · 已索引 18'))
      .toBeVisible()
    expect(document.body.textContent).not.toContain('内容版本 1/0')
  })
})
