import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import type { Model, ModelProvider } from '../types'
import { ModelEditor } from './model-editor'

vi.mock('../api', () => ({ createModel: vi.fn(), updateModel: vi.fn() }))
vi.mock('sonner', () => ({ toast: { success: vi.fn() } }))

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
  model_counts: { total: 2, active: 2, embedding: 1, rerank: 1 },
  status: 'active',
  created_at: '',
  updated_at: '',
}

const rerankModel: Model = {
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
  display_name: 'Reranker',
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

function wrapper(children: React.ReactNode) {
  return (
    <QueryClientProvider client={new QueryClient()}>
      {children}
    </QueryClientProvider>
  )
}

describe('ModelEditor', () => {
  it('offers both model types and dispatches the selected schema', async () => {
    const screen = await render(
      wrapper(<ModelEditor provider={provider} scope='platform' />)
    )
    await expect
      .element(screen.getByRole('radio', { name: /Embedding/ }))
      .toBeVisible()
    await expect
      .element(screen.getByRole('radio', { name: /Rerank/ }))
      .toBeVisible()
    await userEvent.click(screen.getByRole('radio', { name: /Rerank/ }))
    await expect.element(screen.getByLabelText('最大候选文档数')).toBeVisible()
  })

  it('edits by model type without showing a type selector', async () => {
    const screen = await render(
      wrapper(
        <ModelEditor provider={provider} scope='platform' model={rerankModel} />
      )
    )
    await expect.element(screen.getByText('Rerank')).toBeVisible()
    await expect.element(screen.getByRole('radio')).not.toBeInTheDocument()
    await expect.element(screen.getByLabelText('最大候选文档数')).toBeVisible()
  })
})
