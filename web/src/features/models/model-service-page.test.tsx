import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import { ModelServicePage } from './model-service-page'
import type { Model, ModelProvider } from './types'

const provider: ModelProvider = {
  id: 'provider-1',
  scope: 'platform',
  name: 'siliconflow',
  display_name: 'SiliconFlow 平台',
  description: '',
  provider: 'siliconflow',
  config: {},
  credentials_configured: true,
  credential_fields: ['api_key'],
  capabilities: ['embedding', 'rerank'],
  model_counts: { total: 1, active: 1, embedding: 0, rerank: 1 },
  status: 'active',
  created_at: '',
  updated_at: '',
}

const model: Model = {
  id: 'model-1',
  provider_id: provider.id,
  provider: {
    id: provider.id,
    scope: provider.scope,
    name: provider.name,
    display_name: provider.display_name,
    provider: provider.provider,
    status: provider.status,
  },
  name: 'reranker',
  display_name: 'BGE Reranker v2',
  description: '',
  type: 'rerank',
  model_name: 'BAAI/bge-reranker-v2-m3',
  parameters: {},
  status: 'active',
  reference_count: 0,
  available: true,
  created_at: '',
  updated_at: '',
}

describe('ModelServicePage', () => {
  it('shows models by default and changes views through URL state callback', async () => {
    const client = new QueryClient()
    const search = {
      view: 'models',
      type: 'all',
      capability: 'all',
      status: 'all',
      scope: 'all',
      q: '',
    } as const
    client.setQueryData(['model-providers', 'platform', null], [provider])
    client.setQueryData(
      [
        'model-catalog',
        'platform',
        null,
        { type: 'all', status: 'all', scope: 'all', q: '' },
      ],
      [model]
    )
    const onSearchChange = vi.fn()
    const screen = await render(
      <QueryClientProvider client={client}>
        <ModelServicePage
          scope='platform'
          canManage
          search={search}
          onSearchChange={onSearchChange}
        />
      </QueryClientProvider>
    )
    await expect
      .element(screen.getByRole('heading', { name: '模型服务' }))
      .toBeVisible()
    await expect
      .element(screen.getByText('BGE Reranker v2').first())
      .toBeVisible()
    await userEvent.click(screen.getByRole('button', { name: /连接管理/ }))
    expect(onSearchChange).toHaveBeenCalledWith({ view: 'connections' })
  })
})
