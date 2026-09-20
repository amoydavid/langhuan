import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import {
  createSourceConnectionFormSchema,
  toCreateSourceConnectionInput,
} from '../schemas'
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
  testSourceConnection: vi.fn(),
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
      endpoint_url: '',
      status: 'active',
      created_at: '2026-08-01T00:00:00Z',
      updated_at: '2026-08-01T00:00:00Z',
    })
  })

  it('rejects an empty feishu submission', () => {
    expect(
      createSourceConnectionFormSchema.safeParse({
        provider: 'feishu',
        name: '',
        app_id: '',
        app_secret: '',
        endpoint_url: '',
      }).success
    ).toBe(false)
  })

  it('validates feishu fields via zod', () => {
    const valid = {
      provider: 'feishu' as const,
      name: '检索助手',
      app_id: 'cli_a1b2c3d4e5f6',
      app_secret: 'super-secret-value',
      endpoint_url: '',
    }
    expect(createSourceConnectionFormSchema.safeParse(valid).success).toBe(true)
    expect(
      createSourceConnectionFormSchema.safeParse({ ...valid, name: '' }).success
    ).toBe(false)
    expect(
      createSourceConnectionFormSchema.safeParse({ ...valid, app_id: '' })
        .success
    ).toBe(false)
    expect(
      createSourceConnectionFormSchema.safeParse({ ...valid, app_secret: '' })
        .success
    ).toBe(false)
  })

  it('validates per-provider rules when switching provider', () => {
    // firecrawl：API Key 必填、endpoint 可选但必须 http/https。
    const firecrawl = {
      provider: 'firecrawl' as const,
      name: 'fc',
      app_id: '',
      app_secret: 'fc-key',
      endpoint_url: '',
    }
    expect(createSourceConnectionFormSchema.safeParse(firecrawl).success).toBe(
      true
    )
    expect(
      createSourceConnectionFormSchema.safeParse({
        ...firecrawl,
        app_secret: '',
      }).success
    ).toBe(false)
    expect(
      createSourceConnectionFormSchema.safeParse({
        ...firecrawl,
        endpoint_url: 'ws://127.0.0.1:9222',
      }).success
    ).toBe(false)

    // jina：仅 API Key 必填。
    expect(
      createSourceConnectionFormSchema.safeParse({
        provider: 'jina',
        name: 'jina',
        app_id: '',
        app_secret: 'jina-key',
        endpoint_url: '',
      }).success
    ).toBe(true)
    expect(
      createSourceConnectionFormSchema.safeParse({
        provider: 'jina',
        name: 'jina',
        app_id: '',
        app_secret: '',
        endpoint_url: '',
      }).success
    ).toBe(false)

    // browser：endpoint 必填且仅 ws/wss；token 可空。
    const browser = {
      provider: 'browser' as const,
      name: 'cdp',
      app_id: '',
      app_secret: '',
      endpoint_url: 'ws://127.0.0.1:9222',
    }
    expect(createSourceConnectionFormSchema.safeParse(browser).success).toBe(
      true
    )
    expect(
      createSourceConnectionFormSchema.safeParse({
        ...browser,
        endpoint_url: '',
      }).success
    ).toBe(false)
    expect(
      createSourceConnectionFormSchema.safeParse({
        ...browser,
        endpoint_url: 'https://api.firecrawl.dev',
      }).success
    ).toBe(false)
  })

  it('maps form values to the wire payload per provider', () => {
    expect(
      toCreateSourceConnectionInput({
        provider: 'feishu',
        name: ' 检索助手 ',
        app_id: ' cli_a1b2c3d4e5f6 ',
        app_secret: 'super-secret-value',
        endpoint_url: '',
      })
    ).toEqual({
      provider: 'feishu',
      name: '检索助手',
      app_id: 'cli_a1b2c3d4e5f6',
      app_secret: 'super-secret-value',
    })

    expect(
      toCreateSourceConnectionInput({
        provider: 'firecrawl',
        name: 'fc',
        app_id: '',
        app_secret: 'fc-key',
        endpoint_url: '',
      })
    ).toEqual({ provider: 'firecrawl', name: 'fc', app_secret: 'fc-key' })

    expect(
      toCreateSourceConnectionInput({
        provider: 'firecrawl',
        name: 'fc',
        app_id: '',
        app_secret: 'fc-key',
        endpoint_url: 'https://fc.internal',
      })
    ).toEqual({
      provider: 'firecrawl',
      name: 'fc',
      app_secret: 'fc-key',
      config: { endpoint_url: 'https://fc.internal' },
    })

    expect(
      toCreateSourceConnectionInput({
        provider: 'jina',
        name: 'jina',
        app_id: '',
        app_secret: 'jina-key',
        endpoint_url: 'ws://ignored',
      })
    ).toEqual({ provider: 'jina', name: 'jina', app_secret: 'jina-key' })

    expect(
      toCreateSourceConnectionInput({
        provider: 'browser',
        name: 'cdp',
        app_id: '',
        app_secret: '',
        endpoint_url: ' ws://127.0.0.1:9222 ',
      })
    ).toEqual({
      provider: 'browser',
      name: 'cdp',
      config: { endpoint_url: 'ws://127.0.0.1:9222' },
    })

    expect(
      toCreateSourceConnectionInput({
        provider: 'browser',
        name: 'cdp',
        app_id: '',
        app_secret: 'tok',
        endpoint_url: 'ws://127.0.0.1:9222',
      })
    ).toEqual({
      provider: 'browser',
      name: 'cdp',
      app_secret: 'tok',
      config: { endpoint_url: 'ws://127.0.0.1:9222' },
    })
  })

  it('requires name, app id and app secret before feishu submit', async () => {
    const { screen } = await renderForm()

    await userEvent.click(screen.getByRole('button', { name: '保存' }))

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
    await userEvent.click(screen.getByRole('button', { name: '保存' }))

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

  it('renders provider-specific fields when switching to firecrawl', async () => {
    const { screen } = await renderForm()

    // 默认 feishu：App ID / App Secret 可见。
    await expect.element(screen.getByLabelText('App ID')).toBeInTheDocument()

    await userEvent.click(screen.getByText('Firecrawl'))

    // App ID 行随 provider 切换消失，出现 API Key 与可选 Endpoint。
    await expect
      .element(screen.getByLabelText('App ID'))
      .not.toBeInTheDocument()
    await expect.element(screen.getByLabelText('API Key')).toBeInTheDocument()
    await expect
      .element(screen.getByLabelText('Endpoint URL'))
      .toBeInTheDocument()

    await userEvent.fill(screen.getByLabelText('应用名称'), 'fc 主账号')
    await userEvent.click(screen.getByRole('button', { name: '保存' }))
    await expect
      .element(screen.getByText('API Key 不能为空'))
      .toBeInTheDocument()
    expect(createSourceConnection).not.toHaveBeenCalled()

    await userEvent.fill(screen.getByLabelText('API Key'), 'fc-key-value')
    await userEvent.click(screen.getByRole('button', { name: '保存' }))

    await vi.waitFor(() =>
      expect(createSourceConnection).toHaveBeenCalledOnce()
    )
    expect(createSourceConnection).toHaveBeenCalledWith('acme', {
      provider: 'firecrawl',
      name: 'fc 主账号',
      app_secret: 'fc-key-value',
    })
  })

  it('requires a ws/wss endpoint for browser connections', async () => {
    const { screen } = await renderForm()

    await userEvent.click(screen.getByText('渲染浏览器（CDP）'))
    await userEvent.fill(screen.getByLabelText('应用名称'), '渲染浏览器')
    await userEvent.fill(
      screen.getByLabelText('Endpoint URL'),
      'https://wrong.example.com'
    )
    await userEvent.click(screen.getByRole('button', { name: '保存' }))

    await expect
      .element(screen.getByText('CDP 端点仅支持 ws/wss（如 ws://host:9222）'))
      .toBeInTheDocument()
    expect(createSourceConnection).not.toHaveBeenCalled()

    await userEvent.fill(
      screen.getByLabelText('Endpoint URL'),
      'ws://127.0.0.1:9222'
    )
    await userEvent.click(screen.getByRole('button', { name: '保存' }))

    await vi.waitFor(() =>
      expect(createSourceConnection).toHaveBeenCalledOnce()
    )
    expect(createSourceConnection).toHaveBeenCalledWith('acme', {
      provider: 'browser',
      name: '渲染浏览器',
      config: { endpoint_url: 'ws://127.0.0.1:9222' },
    })
  })
})
