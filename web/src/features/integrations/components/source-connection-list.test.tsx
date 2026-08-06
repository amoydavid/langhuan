import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import type { SourceConnection } from '../types'
import { SourceConnectionList } from './source-connection-list'

// 空态会渲染 <Link>，mock 掉避免触发路由上下文依赖。
vi.mock('@tanstack/react-router', () => ({
  Link: ({ children }: { children: React.ReactNode }) => (
    <a href='/test'>{children}</a>
  ),
}))
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

function makeConnection(
  overrides: Partial<SourceConnection> = {}
): SourceConnection {
  return {
    id: '40000000-0000-4000-8000-000000000004',
    workspace_id: '30000000-0000-4000-8000-000000000003',
    provider: 'feishu',
    name: '检索助手',
    app_id: 'cli_a1b2c3d4e5f6',
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

  it('renders connection name and app id', async () => {
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

  it('renders the empty state with an add button when no connections exist', async () => {
    const { screen } = await renderList({ connections: [] })

    await expect
      .element(screen.getByText('尚未添加飞书应用'))
      .toBeInTheDocument()
    await expect
      .element(screen.getByRole('link', { name: '添加飞书应用' }))
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
})
