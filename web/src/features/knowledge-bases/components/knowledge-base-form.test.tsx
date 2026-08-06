import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import type { Role } from '@/features/auth/types'
import type { SourceConnection } from '@/features/integrations/types'
import type { Model } from '@/features/models/types'
import { knowledgeBaseSchema } from '../schemas'
import { KnowledgeBaseForm } from './knowledge-base-form'

const navigate = vi.hoisted(() => vi.fn())
const createKnowledgeBase = vi.hoisted(() => vi.fn())

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    useNavigate: () => navigate,
    // 用最小 <a> 替换 Link，避免在组件测试中依赖 RouterProvider。
    Link: ({
      children,
      to,
      params,
      ...props
    }: React.PropsWithChildren<{
      to: string
      params?: Record<string, string>
    }>) => {
      const href = Object.entries(params ?? {}).reduce<string>(
        (acc, [key, value]) => acc.replace(`$${key}`, value),
        to
      )
      return (
        <a href={href} {...props}>
          {children}
        </a>
      )
    },
  }
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

function activeConnection(
  overrides: Partial<SourceConnection> = {}
): SourceConnection {
  return {
    id: '60000000-0000-4000-8000-000000000006',
    workspace_id: '30000000-0000-4000-8000-000000000003',
    provider: 'feishu',
    name: '检索助手',
    app_id: 'cli_aaaaaaaaaaaa',
    status: 'active',
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
    ...overrides,
  }
}

async function renderForm({
  models = [selectableModel()],
  role = 'member',
  connections = [activeConnection()],
}: {
  models?: Model[]
  role?: Role
  connections?: SourceConnection[]
} = {}) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  client.setQueryData(['models', 'workspace', 'acme', 'selectable'], models)
  client.setQueryData(['source-connections', 'acme'], connections)
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
      source_type: 'upload',
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
      source_type: 'upload' as const,
      source_connection_id: undefined,
      root_token: '',
      sync_enabled: false,
      cron: '',
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

  it('keeps upload as the default source without feishu fields', async () => {
    const { screen } = await renderForm()

    // 默认选中本地上传
    await expect.element(screen.getByLabelText('本地上传')).toBeChecked()
    // 飞书专属字段不渲染
    expect(document.body.textContent).not.toContain('飞书应用')
    expect(document.body.textContent).not.toContain('知识库 Token / 链接')
  })

  it('reveals feishu app and token fields when choosing feishu wiki', async () => {
    const { screen } = await renderForm()

    await userEvent.click(screen.getByLabelText('飞书知识库'))

    await expect.element(screen.getByLabelText('飞书应用')).toBeVisible()
    await expect
      .element(screen.getByLabelText('知识库 Token / 链接'))
      .toBeVisible()
  })

  it('requires a feishu app and token before submitting a feishu source', async () => {
    const { screen } = await renderForm()

    await userEvent.click(screen.getByLabelText('飞书知识库'))
    await userEvent.fill(screen.getByLabelText('名称'), '飞书知识库')
    await userEvent.click(screen.getByRole('button', { name: '创建知识库' }))

    // 未选应用 / 未填 token 报校验错
    await vi.waitFor(() => expect(createKnowledgeBase).not.toHaveBeenCalled())
    await expect.element(screen.getByText('请选择飞书应用')).toBeVisible()
    await expect
      .element(screen.getByText('请输入知识库 Token / 链接'))
      .toBeVisible()
  })

  it('packs source_type, source_connection_id and source_config for feishu', async () => {
    const { screen } = await renderForm()

    await userEvent.fill(screen.getByLabelText('名称'), '飞书知识库')
    await userEvent.click(screen.getByLabelText('飞书云文档'))
    // 选择飞书应用
    await userEvent.click(screen.getByRole('combobox', { name: '飞书应用' }))
    await userEvent.click(screen.getByRole('option', { name: '检索助手' }))
    await userEvent.fill(
      screen.getByLabelText('知识库 Token / 链接'),
      'fldcnXXXX'
    )
    await userEvent.click(screen.getByRole('button', { name: '创建知识库' }))

    await vi.waitFor(() => expect(createKnowledgeBase).toHaveBeenCalledOnce())
    expect(createKnowledgeBase).toHaveBeenCalledWith('acme', {
      name: '飞书知识库',
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
      source_type: 'feishu_drive',
      source_connection_id: '60000000-0000-4000-8000-000000000006',
      source_config: {
        root_token: 'fldcnXXXX',
        root_kind: 'drive_folder',
      },
    })
  })

  it('points to the integrations page when no feishu app exists', async () => {
    const { screen } = await renderForm({ connections: [] })

    await userEvent.click(screen.getByLabelText('飞书知识库'))
    await expect
      .element(screen.getByRole('link', { name: '去添加应用' }))
      .toHaveAttribute('href', '/workspaces/acme/integrations')
  })
})
