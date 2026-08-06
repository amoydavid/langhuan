import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-react'
import type { Model } from '../types'
import { ModelCatalog } from './model-catalog'

const model: Model = {
  id: 'model-1',
  provider_id: 'provider-1',
  provider: {
    id: 'provider-1',
    scope: 'platform',
    name: 'siliconflow',
    display_name: 'SiliconFlow 平台',
    provider: 'siliconflow',
    status: 'disabled',
  },
  name: 'bge_reranker',
  display_name: 'BGE Reranker v2',
  description: '',
  type: 'rerank',
  model_name: 'BAAI/bge-reranker-v2-m3',
  parameters: {},
  status: 'active',
  reference_count: 2,
  available: false,
  created_at: '2026-08-06T00:00:00Z',
  updated_at: '2026-08-06T00:00:00Z',
}

describe('ModelCatalog', () => {
  it('distinguishes a disabled connection from a disabled model', async () => {
    const screen = await render(
      <ModelCatalog
        models={[model]}
        routeScope='workspace'
        workspaceSlug='acme'
      />
    )
    const statuses = screen.getByText('连接已停用')
    await expect.element(statuses.first()).toBeVisible()
    await expect
      .element(screen.getByText('BGE Reranker v2').first())
      .toBeVisible()
  })
})
