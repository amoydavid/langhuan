import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import { syncWebSource } from '../api'
import type { WebSourceView } from '../types'
import { WebSourceCard } from './web-source-card'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  syncWebSource: vi.fn().mockResolvedValue({ job_id: 'job-700' }),
}))
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))
vi.mock('@tanstack/react-router', () => ({
  Link: ({
    children,
    to,
    ...props
  }: React.ComponentProps<'a'> & { to: string }) => (
    <a href={to} {...props}>
      {children}
    </a>
  ),
}))

const mockedSync = vi.mocked(syncWebSource)

const sourceId = '60000000-0000-4000-8000-000000000006'

function makeView(
  overrides: {
    source?: Partial<WebSourceView['source']>
    page_count?: number
  } = {}
): WebSourceView {
  return {
    source: {
      id: sourceId,
      workspace_id: '30000000-0000-4000-8000-000000000003',
      knowledge_base_id: '20000000-0000-4000-8000-000000000002',
      kind: 'web',
      name: '产品文档站',
      status: 'active',
      config: {
        mode: 'crawl',
        crawl: { seed_url: 'https://docs.example.com/' },
      },
      sync_cursor: {},
      next_sync_at: null,
      sync_requested_force: false,
      sync_last_result: { status: 'succeeded', synced_documents: 12 },
      created_at: '2026-09-01T00:00:00Z',
      updated_at: '2026-09-16T08:00:00Z',
      ...overrides.source,
    },
    page_count: overrides.page_count ?? 486,
    failed_page_count: 3,
  }
}

async function renderCard({
  view = makeView(),
  canManage = true,
}: {
  view?: WebSourceView
  canManage?: boolean
} = {}) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const screen = await render(
    <QueryClientProvider client={client}>
      <WebSourceCard
        view={view}
        workspaceSlug='acme'
        kbId='kb-1'
        canManage={canManage}
      />
    </QueryClientProvider>
  )
  return { screen }
}

describe('WebSourceCard 强制重爬（v1.4.0 §9.3.7）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockedSync.mockResolvedValue({ job_id: 'job-700' })
  })

  it('shows the force re-crawl action for managers and hides it for members', async () => {
    const managed = await renderCard({ canManage: true })
    await expect
      .element(managed.screen.getByRole('button', { name: '强制重爬' }))
      .toBeVisible()
    await managed.screen.unmount()

    const readonly = await renderCard({ canManage: false })
    await expect
      .element(readonly.screen.getByRole('button', { name: '强制重爬' }))
      .not.toBeInTheDocument()
  })

  it('queues a force re-crawl with force=true only after the cost confirmation', async () => {
    const { screen } = await renderCard()

    await userEvent.click(screen.getByRole('button', { name: '强制重爬' }))
    await expect
      .element(screen.getByText(/重新抓取全部 486 个页面并生成新版本/))
      .toBeVisible()
    await expect
      .element(screen.getByText(/仅在怀疑上游变更检测异常时使用/))
      .toBeVisible()
    // 打开确认框但未确认前不触发请求。
    expect(mockedSync).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: '开始强制重爬' }))
    await vi.waitFor(() => expect(mockedSync).toHaveBeenCalledTimes(1))
    expect(mockedSync).toHaveBeenCalledWith('acme', 'kb-1', sourceId, true)
  })

  it('keeps the sync-now action on force=false', async () => {
    const { screen } = await renderCard()

    await userEvent.click(screen.getByRole('button', { name: '立即同步' }))
    await vi.waitFor(() => expect(mockedSync).toHaveBeenCalledTimes(1))
    expect(mockedSync).toHaveBeenCalledWith('acme', 'kb-1', sourceId, false)
  })
})
