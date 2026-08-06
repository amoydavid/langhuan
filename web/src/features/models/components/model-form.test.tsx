import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import type { ModelProvider } from '../types'
import { ModelForm } from './model-form'

const createModel = vi.hoisted(() => vi.fn())
const updateModel = vi.hoisted(() => vi.fn())
const listProviderModelCatalog = vi.hoisted(() => vi.fn())

vi.mock('../api', () => ({
  createModel,
  updateModel,
  listProviderModelCatalog,
}))
vi.mock('sonner', () => ({ toast: { success: vi.fn() } }))

const provider: ModelProvider = {
  id: 'provider-1',
  scope: 'workspace',
  workspace_id: 'workspace-1',
  name: 'dashscope-prod',
  display_name: 'DashScope Production',
  description: '',
  provider: 'dashscope',
  config: { timeout_seconds: 60 },
  credentials_configured: true,
  credential_fields: ['api_key'],
  capabilities: ['embedding'],
  model_counts: { total: 0, active: 0, embedding: 0, rerank: 0 },
  status: 'active',
  created_at: '2026-07-30T00:00:00Z',
  updated_at: '2026-07-30T00:00:00Z',
}

describe('ModelForm provider constraints', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    createModel.mockResolvedValue({ id: 'model-1' })
    listProviderModelCatalog.mockResolvedValue({ items: [] })
  })

  it('locks DashScope to 1024 dimensions', async () => {
    const client = new QueryClient()
    const screen = await render(
      <QueryClientProvider client={client}>
        <ModelForm provider={provider} scope='workspace' workspaceSlug='acme' />
      </QueryClientProvider>
    )
    await expect.element(screen.getByLabelText('向量维度')).toHaveValue('1024')
    await expect.element(screen.getByLabelText('向量维度')).toBeDisabled()
  })

  it('shows the validation error when the model identifier is invalid', async () => {
    const client = new QueryClient()
    const screen = await render(
      <QueryClientProvider client={client}>
        <ModelForm provider={provider} scope='workspace' workspaceSlug='acme' />
      </QueryClientProvider>
    )
    await userEvent.fill(
      screen.getByLabelText('模型标识'),
      'Qwen3-Embedding-0.6B'
    )
    await userEvent.fill(
      screen.getByLabelText('显示名称'),
      'Qwen3-Embedding-0.6B'
    )
    await userEvent.fill(
      screen.getByLabelText('供应商模型名称'),
      'Qwen/Qwen3-Embedding-0.6B'
    )
    await userEvent.click(screen.getByRole('button', { name: '保存模型' }))

    await expect.element(screen.getByText('请输入小写模型标识')).toBeVisible()
    expect(createModel).not.toHaveBeenCalled()
  })

  it('invalidates the workspace selectable-model cache after save', async () => {
    const client = new QueryClient()
    const invalidateQueries = vi.spyOn(client, 'invalidateQueries')
    const screen = await render(
      <QueryClientProvider client={client}>
        <ModelForm provider={provider} scope='workspace' workspaceSlug='acme' />
      </QueryClientProvider>
    )
    await userEvent.fill(screen.getByLabelText('模型标识'), 'embedding-v4')
    await userEvent.fill(screen.getByLabelText('显示名称'), 'Embedding V4')
    await userEvent.fill(
      screen.getByLabelText('供应商模型名称'),
      'text-embedding-v4'
    )
    await userEvent.click(screen.getByRole('button', { name: '保存模型' }))

    await vi.waitFor(() => expect(createModel).toHaveBeenCalledOnce())
    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: ['models', 'workspace', 'acme', 'selectable'],
    })
  })

  it('fills editable fields from the provider catalog without submitting', async () => {
    listProviderModelCatalog.mockResolvedValue({
      items: [
        {
          id: 'BAAI/bge-m3',
          display_name: 'BGE M3',
          description: 'Embedding model',
          type: 'embedding',
          dimensions: 1024,
          parameters: { batch_size: 64 },
          available: true,
        },
      ],
    })
    const client = new QueryClient()
    const screen = await render(
      <QueryClientProvider client={client}>
        <ModelForm
          provider={{
            ...provider,
            provider: 'siliconflow',
            model_catalog: true,
          }}
          scope='workspace'
          workspaceSlug='acme'
        />
      </QueryClientProvider>
    )

    await userEvent.click(
      screen.getByRole('button', {
        name: '从 Provider 模型目录快速填充',
      })
    )
    await userEvent.click(screen.getByRole('button', { name: /BGE M3/ }))

    await expect
      .element(screen.getByLabelText('供应商模型名称'))
      .toHaveValue('BAAI/bge-m3')
    await expect
      .element(screen.getByLabelText('显示名称'))
      .toHaveValue('BGE M3')
    await expect.element(screen.getByLabelText('批量大小')).toHaveValue(64)
    expect(createModel).not.toHaveBeenCalled()
  })
})
