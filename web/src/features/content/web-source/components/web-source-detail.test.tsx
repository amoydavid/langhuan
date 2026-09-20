import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import type { WebSourceView } from '../types'
import { WebSourceDetail } from './web-source-detail'

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
  useNavigate: () => vi.fn(),
}))
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

const sourceId = '60000000-0000-4000-8000-000000000006'

function makeView(
  overrides: { source?: Partial<WebSourceView['source']> } = {}
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
    page_count: 486,
    failed_page_count: 3,
  }
}

async function renderDetail({
  view = makeView(),
  canManage = true,
}: {
  view?: WebSourceView
  canManage?: boolean
} = {}) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  client.setQueryData(['web-sources', 'acme', 'kb-1', sourceId], view)
  client.setQueryData(
    ['documents', 'acme', 'kb-1', { kb_source_id: sourceId }],
    []
  )
  const screen = await render(
    <QueryClientProvider client={client}>
      <WebSourceDetail
        workspaceSlug='acme'
        kbId='kb-1'
        sourceId={sourceId}
        canManage={canManage}
      />
    </QueryClientProvider>
  )
  return { screen }
}

describe('WebSourceDetail', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('shows the source summary and pages list heading for readers', async () => {
    const { screen } = await renderDetail({ canManage: false })
    await expect.element(screen.getByText('产品文档站')).toBeVisible()
    await expect
      .element(screen.getByRole('link', { name: '返回 Web 列表' }))
      .toBeVisible()
    await expect.element(screen.getByText('该请求抓到的页面 486')).toBeVisible()
  })

  it('hides every write action for members (list-page parity)', async () => {
    const { screen } = await renderDetail({ canManage: false })
    for (const name of ['立即同步', '编辑配置', '停用', '删除']) {
      await expect
        .element(screen.getByRole('button', { name }))
        .not.toBeInTheDocument()
    }
    // urls 模式专属的「管理 URL」同样不可见。
    const urlsView = makeView({
      source: {
        config: {
          mode: 'urls',
          urls: ['https://example.com/a'],
          on_delete: 'keep',
        },
      },
    })
    const urls = await renderDetail({ view: urlsView, canManage: false })
    await expect
      .element(urls.screen.getByRole('button', { name: '管理 URL' }))
      .not.toBeInTheDocument()
  })

  it('shows management actions for managers, gated by mode', async () => {
    const { screen } = await renderDetail({ canManage: true })
    await expect
      .element(screen.getByRole('button', { name: '立即同步' }))
      .toBeVisible()
    await expect
      .element(screen.getByRole('button', { name: '编辑配置' }))
      .toBeVisible()
    await expect
      .element(screen.getByRole('button', { name: '停用' }))
      .toBeVisible()
    await expect
      .element(screen.getByRole('button', { name: '删除' }))
      .toBeVisible()
    // crawl 模式不出现「管理 URL」。
    await expect
      .element(screen.getByRole('button', { name: '管理 URL' }))
      .not.toBeInTheDocument()

    const urlsDetail = await renderDetail({
      canManage: true,
      view: makeView({
        source: {
          config: {
            mode: 'urls',
            urls: ['https://example.com/a'],
            on_delete: 'keep',
          },
        },
      }),
    })
    await expect
      .element(urlsDetail.screen.getByRole('button', { name: '管理 URL' }))
      .toBeVisible()
  })
})
