import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-react'
import type { Model } from '../types'
import { ModelCard } from './model-card'

const model: Model = {
  id: 'model-1',
  provider_id: 'provider-1',
  provider: {
    id: 'provider-1',
    scope: 'workspace',
    workspace_id: 'workspace-1',
    name: 'dashscope-prod',
    display_name: 'DashScope Production',
    provider: 'dashscope',
    status: 'active',
  },
  name: 'text-embedding-v4',
  display_name: '文本向量 v4',
  description: '',
  type: 'embedding',
  model_name: 'text-embedding-v4',
  dimensions: 1024,
  parameters: { batch_size: 32 },
  status: 'disabled',
  reference_count: 3,
  available: false,
  created_at: '2026-07-30T00:00:00Z',
  updated_at: '2026-07-30T00:00:00Z',
}

describe('ModelCard', () => {
  it('keeps disabled models visible with dimensions and references', async () => {
    const screen = await render(<ModelCard model={model} canManage={false} />)

    await expect.element(screen.getByText('已停用')).toBeInTheDocument()
    await expect.element(screen.getByText('1024 维')).toBeInTheDocument()
    await expect.element(screen.getByText('3 个知识库引用')).toBeInTheDocument()
  })
})
