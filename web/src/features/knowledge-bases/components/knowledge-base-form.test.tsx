import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import type { Role } from '@/features/auth/types'
import type { Model } from '@/features/models/types'
import { knowledgeBaseSchema } from '../schemas'
import { KnowledgeBaseForm } from './knowledge-base-form'

const navigate = vi.hoisted(() => vi.fn())
const createKnowledgeBase = vi.hoisted(() => vi.fn())

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return { ...actual, useNavigate: () => navigate }
})
vi.mock('../api', () => ({ createKnowledgeBase }))
vi.mock('sonner', () => ({ toast: { success: vi.fn() } }))

function selectableModel(overrides: Partial<Model> = {}): Model {
  return {
    id: '20000000-0000-4000-8000-000000000002',
    provider_id: '10000000-0000-4000-8000-000000000001',
    provider: {
      id: '10000000-0000-4000-8000-000000000001',
      scope: 'workspace',
      workspace_id: '30000000-0000-4000-8000-000000000003',
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
    status: 'active',
    reference_count: 0,
    available: true,
    created_at: '2026-07-30T00:00:00Z',
    updated_at: '2026-07-30T00:00:00Z',
    ...overrides,
  }
}

async function renderForm({
  models = [selectableModel()],
  role = 'member',
}: {
  models?: Model[]
  role?: Role
} = {}) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  client.setQueryData(['models', 'workspace', 'acme', 'selectable'], models)
  const invalidateQueries = vi.spyOn(client, 'invalidateQueries')
  const screen = await render(
    <QueryClientProvider client={client}>
      <KnowledgeBaseForm workspaceSlug='acme' workspaceRole={role} />
    </QueryClientProvider>
  )
  return { screen, invalidateQueries }
}

describe('KnowledgeBaseForm', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    createKnowledgeBase.mockResolvedValue({
      id: '40000000-0000-4000-8000-000000000004',
      workspace_id: '30000000-0000-4000-8000-000000000003',
      name: '产品文档',
      description: '',
      embedding_model_id: '20000000-0000-4000-8000-000000000002',
      embedding_model: {
        id: '20000000-0000-4000-8000-000000000002',
        name: 'text-embedding-v4',
        display_name: '文本向量 v4',
        provider: 'dashscope',
        provider_display_name: 'DashScope Production',
        dimensions: 1024,
        available: true,
      },
      chunking_config: {
        strategy: 'auto',
        enable_parent_child: true,
        parent_chunk_size: 4096,
        child_chunk_size: 384,
        chunk_size: 512,
        chunk_overlap: 80,
      },
      metadata: {},
      created_at: '2026-07-30T00:00:00Z',
      updated_at: '2026-07-30T00:00:00Z',
    })
  })

  it('validates model identity and chunking', () => {
    const base = {
      name: '产品文档',
      description: '',
      embedding_model_id: '20000000-0000-4000-8000-000000000002',
      strategy: 'auto' as const,
      enable_parent_child: true,
      parent_chunk_size: 4096,
      child_chunk_size: 384,
      chunk_size: 512,
      chunk_overlap: 80,
    }

    expect(
      knowledgeBaseSchema.safeParse({ ...base, embedding_model_id: '' }).success
    ).toBe(false)
    expect(
      knowledgeBaseSchema.safeParse({ ...base, chunk_overlap: 4096 }).success
    ).toBe(false)
    expect(knowledgeBaseSchema.safeParse(base).success).toBe(true)
  })

  it('auto-selects the only model and submits only its id', async () => {
    const { screen, invalidateQueries } = await renderForm()
    await userEvent.fill(screen.getByLabelText('名称'), '产品文档')
    await userEvent.click(screen.getByRole('button', { name: '创建知识库' }))

    await vi.waitFor(() => expect(createKnowledgeBase).toHaveBeenCalledOnce())
    expect(createKnowledgeBase).toHaveBeenCalledWith('acme', {
      name: '产品文档',
      description: '',
      embedding_model_id: '20000000-0000-4000-8000-000000000002',
      chunking_config: {
        strategy: 'auto',
        enable_parent_child: true,
        parent_chunk_size: 4096,
        child_chunk_size: 384,
        chunk_size: 512,
        chunk_overlap: 80,
      },
    })
    expect(createKnowledgeBase.mock.calls[0]?.[1]).not.toHaveProperty(
      ['embedding', 'dimension'].join('_')
    )
    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: ['knowledge-bases', 'acme'],
    })
  })

  it('requires an explicit choice when multiple models are available', async () => {
    const second = selectableModel({
      id: '50000000-0000-4000-8000-000000000005',
      display_name: '平台 OpenAI Embedding',
      provider: {
        ...selectableModel().provider,
        scope: 'platform',
        display_name: '平台 OpenAI',
        provider: 'openai',
      },
    })
    const { screen } = await renderForm({ models: [selectableModel(), second] })

    await expect
      .element(screen.getByLabelText('Embedding 模型'))
      .toHaveValue('')
  })

  it('guides an administrator to model configuration when none exist', async () => {
    const { screen } = await renderForm({ models: [], role: 'admin' })

    await expect
      .element(screen.getByRole('link', { name: '先配置模型' }))
      .toHaveAttribute('href', '/workspaces/acme/models')
    await expect
      .element(screen.getByRole('button', { name: '创建知识库' }))
      .toBeDisabled()
  })

  it('asks a member to contact an administrator when none exist', async () => {
    const { screen } = await renderForm({ models: [], role: 'member' })

    await expect
      .element(screen.getByText('请联系 Workspace 管理员配置模型。'))
      .toBeInTheDocument()
  })
})
