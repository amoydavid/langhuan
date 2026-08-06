import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import { createSourceConnectionSchema } from '../schemas'
import { SourceConnectionForm } from './source-connection-form'

const navigate = vi.hoisted(() => vi.fn())
const createSourceConnection = vi.hoisted(() => vi.fn())

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return { ...actual, useNavigate: () => navigate }
})
// queries.ts 顶层会 import api.ts 的全部具名导出，mock 必须覆盖完整签名。
vi.mock('../api', () => ({
  createSourceConnection,
  deleteSourceConnection: vi.fn(),
  getSourceConnection: vi.fn(),
  listSourceConnections: vi.fn(),
  updateSourceConnection: vi.fn(),
}))
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

async function renderForm() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const invalidateQueries = vi.spyOn(client, 'invalidateQueries')
  const screen = await render(
    <QueryClientProvider client={client}>
      <SourceConnectionForm workspaceSlug='acme' />
    </QueryClientProvider>
  )
  return { screen, invalidateQueries }
}

describe('SourceConnectionForm', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    createSourceConnection.mockResolvedValue({
      id: '40000000-0000-4000-8000-000000000004',
      workspace_id: '30000000-0000-4000-8000-000000000003',
      provider: 'feishu',
      name: '检索助手',
      app_id: 'cli_a1b2c3d4e5f6',
      status: 'active',
      created_at: '2026-08-01T00:00:00Z',
      updated_at: '2026-08-01T00:00:00Z',
    })
  })

  it('rejects an empty submission', () => {
    expect(
      createSourceConnectionSchema.safeParse({
        provider: 'feishu',
        name: '',
        app_id: '',
        app_secret: '',
      }).success
    ).toBe(false)
  })

  it('validates required fields via zod', () => {
    const valid = {
      provider: 'feishu' as const,
      name: '检索助手',
      app_id: 'cli_a1b2c3d4e5f6',
      app_secret: 'super-secret-value',
    }
    expect(createSourceConnectionSchema.safeParse(valid).success).toBe(true)
    expect(
      createSourceConnectionSchema.safeParse({ ...valid, name: '' }).success
    ).toBe(false)
    expect(
      createSourceConnectionSchema.safeParse({ ...valid, app_id: '' }).success
    ).toBe(false)
    expect(
      createSourceConnectionSchema.safeParse({ ...valid, app_secret: '' })
        .success
    ).toBe(false)
  })

  it('requires name, app id and app secret before submit', async () => {
    const { screen } = await renderForm()

    await userEvent.click(screen.getByRole('button', { name: '测试并保存' }))

    await expect
      .element(screen.getByText('应用名称不能为空'))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('App ID 不能为空'))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('App Secret 不能为空'))
      .toBeInTheDocument()
    expect(createSourceConnection).not.toHaveBeenCalled()
  })

  it('submits the feishu payload with provider pinned to feishu', async () => {
    const { screen, invalidateQueries } = await renderForm()

    await userEvent.fill(screen.getByLabelText('应用名称'), '检索助手')
    await userEvent.fill(screen.getByLabelText('App ID'), 'cli_a1b2c3d4e5f6')
    await userEvent.fill(
      screen.getByLabelText('App Secret'),
      'super-secret-value'
    )
    await userEvent.click(screen.getByRole('button', { name: '测试并保存' }))

    await vi.waitFor(() =>
      expect(createSourceConnection).toHaveBeenCalledOnce()
    )
    expect(createSourceConnection).toHaveBeenCalledWith('acme', {
      provider: 'feishu',
      name: '检索助手',
      app_id: 'cli_a1b2c3d4e5f6',
      app_secret: 'super-secret-value',
    })
    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: ['source-connections', 'acme'],
    })
    expect(navigate).toHaveBeenCalled()
  })
})
