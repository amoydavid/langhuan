import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import { ApiError } from '@/lib/api/error'
import type { SourceConnection, SourceProvider } from '../types'
import { SourceConnectionList } from './source-connection-list'

// 空态会渲染 <Link>，mock 掉避免触发路由上下文依赖。
vi.mock('@tanstack/react-router', () => ({
  Link: ({ children }: { children: React.ReactNode }) => (
    <a href='/test'>{children}</a>
  ),
}))
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

const testSourceConnection = vi.hoisted(() => vi.fn())
// 卡片内的连接测试会通过 queries.ts 调到 api.ts，mock 全部签名以隔离网络。
vi.mock('../api', () => ({
  createSourceConnection: vi.fn(),
  deleteSourceConnection: vi.fn(),
  getSourceConnection: vi.fn(),
  listSourceConnections: vi.fn(),
  testSourceConnection,
  updateSourceConnection: vi.fn(),
}))

function makeConnection(
  overrides: Partial<SourceConnection> & { provider?: SourceProvider } = {}
): SourceConnection {
  return {
    id: '40000000-0000-4000-8000-000000000004',
    workspace_id: '30000000-0000-4000-8000-000000000003',
    provider: 'feishu',
    name: '检索助手',
    app_id: 'cli_a1b2c3d4e5f6',
    endpoint_url: '',
    status: 'active',
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
    ...overrides,
  }
}

async function renderList({
  connections = [makeConnection()],
  role = 'admin',
}: {
  connections?: SourceConnection[]
  role?: 'owner' | 'admin' | 'member'
} = {}) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  client.setQueryData(['source-connections', 'acme'], connections)
  const screen = await render(
    <QueryClientProvider client={client}>
      <SourceConnectionList workspaceSlug='acme' workspaceRole={role} />
    </QueryClientProvider>
  )
  return { screen, client }
}

describe('SourceConnectionList', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders connection name and app id for feishu', async () => {
    const { screen } = await renderList()

    await expect.element(screen.getByText('检索助手')).toBeInTheDocument()
    await expect
      .element(screen.getByText('cli_a1b2c3d4e5f6'))
      .toBeInTheDocument()
  })

  it('does not expose app_secret anywhere in the DOM', async () => {
    const { screen } = await renderList()

    const container = screen.container
    expect(container.querySelector('app_secret')).toBeNull()
    expect(container.textContent).not.toContain('app_secret')
    // DTO 本身没有 secret 字段，确保不渲染任何占位密钥
    expect(container.textContent).not.toContain('secret_value')
  })

  it('shows the enabled status badge for an active connection', async () => {
    const { screen } = await renderList()

    await expect.element(screen.getByText('已启用')).toBeInTheDocument()
  })

  it('shows the disabled status badge for a disabled connection', async () => {
    const { screen } = await renderList({
      connections: [makeConnection({ status: 'disabled' })],
    })

    await expect.element(screen.getByText('已停用')).toBeInTheDocument()
  })

  it('renders the empty state mentioning all providers when no connections exist', async () => {
    const { screen } = await renderList({ connections: [] })

    await expect
      .element(screen.getByText('尚未添加任何连接'))
      .toBeInTheDocument()
    await expect.element(screen.getByText(/Firecrawl/)).toBeInTheDocument()
    await expect
      .element(screen.getByRole('link', { name: '新建连接' }))
      .toBeInTheDocument()
  })

  it('shows skeleton placeholders while loading', async () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    // 不预置数据，强制处于 isPending 状态
    const screen = await render(
      <QueryClientProvider client={client}>
        <SourceConnectionList workspaceSlug='acme' workspaceRole='admin' />
      </QueryClientProvider>
    )

    await vi.waitFor(() => {
      expect(
        screen.container.querySelectorAll('[data-slot="skeleton"]').length
      ).toBeGreaterThan(0)
    })
  })

  it('hides management buttons for members', async () => {
    const { screen } = await renderList({ role: 'member' })

    await expect
      .element(screen.getByRole('button', { name: '编辑' }))
      .not.toBeInTheDocument()
    await expect
      .element(screen.getByRole('button', { name: '停用' }))
      .not.toBeInTheDocument()
  })

  it('shows management buttons for admins', async () => {
    const { screen } = await renderList({ role: 'admin' })

    await expect
      .element(screen.getByRole('button', { name: '编辑' }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByRole('button', { name: '停用' }))
      .toBeInTheDocument()
  })

  it('replaces the app id row with endpoint rows for firecrawl cards', async () => {
    const { screen } = await renderList({
      connections: [
        makeConnection({
          provider: 'firecrawl',
          name: 'fc 主账号',
          app_id: '',
          endpoint_url: 'https://fc.internal',
        }),
      ],
    })

    await expect.element(screen.getByText('fc 主账号')).toBeInTheDocument()
    // 非飞书卡片不再展示 App ID 行，展示 provider badge 与 endpoint 行。
    await expect
      .element(screen.getByText('cli_a1b2c3d4e5f6'))
      .not.toBeInTheDocument()
    await expect
      .element(screen.getByText('https://fc.internal'))
      .toBeInTheDocument()
    await expect.element(screen.getByText('Firecrawl')).toBeInTheDocument()
  })

  it('falls back to the official default hint when firecrawl has no endpoint', async () => {
    const { screen } = await renderList({
      connections: [
        makeConnection({
          provider: 'firecrawl',
          name: 'fc 云端',
          app_id: '',
          endpoint_url: '',
        }),
      ],
    })

    await expect.element(screen.getByText('官方默认端点')).toBeInTheDocument()
  })

  it('shows the endpoint row for browser cards', async () => {
    const { screen } = await renderList({
      connections: [
        makeConnection({
          provider: 'browser',
          name: '本地 CDP 浏览器',
          app_id: '',
          endpoint_url: 'ws://127.0.0.1:9222',
        }),
      ],
    })

    await expect
      .element(screen.getByText('ws://127.0.0.1:9222'))
      .toBeInTheDocument()
    // 卡片名已避开「渲染浏览器」，此处唯一匹配 provider 徽标。
    await expect.element(screen.getByText('渲染浏览器')).toBeInTheDocument()
  })

  it('renders a successful connection test result', async () => {
    testSourceConnection.mockResolvedValue({
      ok: true,
      provider: 'firecrawl',
      duration_ms: 123,
      message: '连接成功（firecrawl，耗时 123 ms）',
    })
    const { screen } = await renderList({
      connections: [
        makeConnection({
          provider: 'firecrawl',
          name: 'fc 主账号',
          app_id: '',
          endpoint_url: 'https://fc.internal',
        }),
      ],
    })

    await userEvent.click(screen.getByRole('button', { name: '测试连接' }))

    await vi.waitFor(() => expect(testSourceConnection).toHaveBeenCalledOnce())
    expect(testSourceConnection).toHaveBeenCalledWith(
      'acme',
      '40000000-0000-4000-8000-000000000004'
    )
    await expect
      .element(screen.getByText('连接成功（firecrawl，耗时 123 ms）'))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('连接成功（firecrawl，耗时 123 ms）'))
      .toHaveClass('text-emerald-600')
  })

  it('renders a failed connection test result without throwing', async () => {
    // 契约：探测失败也是 HTTP 200 + ok:false，前端按 ok 分支展示为失败。
    testSourceConnection.mockResolvedValue({
      ok: false,
      provider: 'browser',
      duration_ms: 45,
      message: 'CDP 握手失败：connection refused',
    })
    const { screen } = await renderList({
      connections: [
        makeConnection({
          provider: 'browser',
          name: '渲染浏览器',
          app_id: '',
          endpoint_url: 'ws://127.0.0.1:9222',
        }),
      ],
    })

    await userEvent.click(screen.getByRole('button', { name: '测试连接' }))

    await expect
      .element(screen.getByText('CDP 握手失败：connection refused'))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('CDP 握手失败：connection refused'))
      .toHaveClass('text-destructive')
  })

  it('shows an inline error when the connection cannot be tested (disabled)', async () => {
    // 契约：已停用/feishu 不支持 → 400 error body；ApiError 原样透传 message。
    testSourceConnection.mockRejectedValue(
      new ApiError('连接已停用，启用后才能测试', 400, 'validation_error')
    )
    const { screen } = await renderList({
      connections: [
        makeConnection({
          provider: 'jina',
          name: 'jina 主账号',
          app_id: '',
          status: 'disabled',
        }),
      ],
    })

    await userEvent.click(screen.getByRole('button', { name: '测试连接' }))

    await expect.element(screen.getByText(/连接已停用/)).toBeInTheDocument()
  })

  it('does not show the test button for feishu cards', async () => {
    const { screen } = await renderList()

    await expect
      .element(screen.getByRole('button', { name: '测试连接' }))
      .not.toBeInTheDocument()
  })

  it('hides the test button from members', async () => {
    const { screen } = await renderList({
      connections: [
        makeConnection({
          provider: 'jina',
          name: 'jina 主账号',
          app_id: '',
        }),
      ],
      role: 'member',
    })

    await expect
      .element(screen.getByRole('button', { name: '测试连接' }))
      .not.toBeInTheDocument()
  })
})
