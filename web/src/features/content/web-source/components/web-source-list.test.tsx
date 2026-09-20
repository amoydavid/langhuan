import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import type { WebSourceView } from '../types'
import { WebSourceList } from './web-source-list'

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
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

const sourceId = '60000000-0000-4000-8000-000000000006'

function makeView(
  overrides: {
    source?: Partial<WebSourceView['source']>
    page_count?: number
    failed_page_count?: number
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
      next_sync_at: '2026-09-16T14:00:00Z',
      sync_requested_force: false,
      sync_last_result: {
        status: 'succeeded',
        synced_documents: 12,
        deleted_documents: 1,
        failed_documents: 0,
        finished_at: '2026-09-16T08:00:00Z',
      },
      created_at: '2026-09-01T00:00:00Z',
      updated_at: '2026-09-16T08:00:00Z',
      ...overrides.source,
    },
    page_count: overrides.page_count ?? 486,
    failed_page_count: overrides.failed_page_count ?? 3,
  }
}

async function renderList({
  views = [makeView()],
  canManage = true,
}: {
  views?: WebSourceView[]
  canManage?: boolean
} = {}) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  client.setQueryData(['web-sources', 'acme', 'kb-1'], views)
  const screen = await render(
    <QueryClientProvider client={client}>
      <WebSourceList workspaceSlug='acme' kbId='kb-1' canManage={canManage} />
    </QueryClientProvider>
  )
  return { screen, unmount: screen.unmount.bind(screen) }
}

describe('WebSourceList', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders source cards with name, mode badge and page counts', async () => {
    const { screen } = await renderList()
    await expect.element(screen.getByText('产品文档站')).toBeVisible()
    await expect.element(screen.getByText('爬取')).toBeVisible()
    await expect.element(screen.getByText(/页面 486/)).toBeVisible()
    await expect.element(screen.getByText(/失败 3/)).toBeVisible()
  })

  it('renders the last sync summary with non-zero counters only', async () => {
    const { screen } = await renderList()
    await expect.element(screen.getByText(/同步 12 · 删除 1/)).toBeVisible()
  })

  it('derives the health badge from status and last sync result', async () => {
    const { screen } = await renderList({
      views: [
        makeView(),
        makeView({
          source: {
            id: `${sourceId}f`,
            name: '竞品帮助中心',
            status: 'disabled',
          },
          page_count: 41,
        }),
        makeView({
          source: {
            id: `${sourceId}e`,
            name: '失败源',
            sync_last_result: { status: 'failed' },
          },
        }),
      ],
    })
    await expect.element(screen.getByText('正常')).toBeVisible()
    await expect.element(screen.getByText('已停用')).toBeVisible()
    await expect
      .element(screen.getByText('失败', { exact: true }))
      .toBeVisible()
  })

  it('shows the sync action for managers and hides it for members', async () => {
    const managed = await renderList({ canManage: true })
    await expect
      .element(managed.screen.getByRole('button', { name: '立即同步' }))
      .toBeVisible()
    await expect
      .element(managed.screen.getByRole('button', { name: '添加网站源' }))
      .toBeVisible()
    await managed.unmount()

    const readonly = await renderList({ canManage: false })
    await expect
      .element(readonly.screen.getByRole('button', { name: '立即同步' }))
      .not.toBeInTheDocument()
    await expect
      .element(readonly.screen.getByRole('button', { name: '添加网站源' }))
      .not.toBeInTheDocument()
  })

  it('renders the empty state with the spec copy for managers', async () => {
    const { screen } = await renderList({ views: [] })
    await expect.element(screen.getByText('还没有网站内容源')).toBeVisible()
    await expect
      .element(
        screen.getByText('添加后，抓取的网页会与本库上传的文件一起被检索。')
      )
      .toBeVisible()
    await expect
      .element(screen.getByRole('button', { name: '添加网站源' }))
      .toBeVisible()
  })

  it('hides the empty-state add button for members', async () => {
    const { screen } = await renderList({ views: [], canManage: false })
    await expect
      .element(screen.getByRole('button', { name: '添加网站源' }))
      .not.toBeInTheDocument()
  })
})
