import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import { ApiError } from '@/lib/api/error'
import {
  fetchActivity,
  fetchInsightsOverview,
  fetchSleepingDocuments,
  fetchZeroRecallQueries,
} from '../api'
import type {
  InsightsActivityResponse,
  InsightsOverview,
  SleepingDocumentsResponse,
  ZeroRecallQueriesResponse,
} from '../types'
import { InsightsPage } from './insights-page'

// 只替换 insights 的四个取数函数（queries.ts 会拿到这些 mock），常量保持真实值。
vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  fetchInsightsOverview: vi.fn(),
  fetchZeroRecallQueries: vi.fn(),
  fetchSleepingDocuments: vi.fn(),
  fetchActivity: vi.fn(),
}))

const kbId = '20000000-0000-4000-8000-000000000002'

const overview: InsightsOverview = {
  window_days: 14,
  runs_total: 5,
  runs_available: 4,
  runs_empty: 1,
  runs_degraded: 0,
  runs_failed_excluded: 0,
  empty_recall_rate: 0.2,
  previous_empty_recall_rate: 0.3,
  has_previous_window: true,
  sleeping_documents_count: 2,
  sleeping_insufficient_sample: false,
  recent_changes_count: 3,
  query_log_enabled: true,
}

const emptyZeroRecall: ZeroRecallQueriesResponse = {
  query_log_enabled: true,
  window_days: 14,
  queries: [],
}

const sleeping: SleepingDocumentsResponse = {
  insufficient_sample: false,
  documents: [],
}

const activity: InsightsActivityResponse = { events: [] }

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <InsightsPage
        workspaceSlug='acme'
        kbId={kbId}
        windowDays={14}
        onWindowChange={() => {}}
      />
    </QueryClientProvider>
  )
}

describe('InsightsPage 查询错误态', () => {
  beforeEach(() => {
    vi.mocked(fetchInsightsOverview).mockReset().mockResolvedValue(overview)
    vi.mocked(fetchZeroRecallQueries)
      .mockReset()
      .mockResolvedValue(emptyZeroRecall)
    vi.mocked(fetchSleepingDocuments).mockReset().mockResolvedValue(sleeping)
    vi.mocked(fetchActivity).mockReset().mockResolvedValue(activity)
  })

  it('overview 非 403 失败时渲染可重试错误块而不是永久骨架', async () => {
    vi.mocked(fetchInsightsOverview).mockRejectedValue(
      new Error('network down')
    )
    const screen = await renderPage()

    await expect
      .element(screen.getByTestId('insights-panel-error'))
      .toBeVisible()
    await expect.element(screen.getByText('加载失败')).toBeVisible()
    await expect
      .element(screen.getByRole('button', { name: '重试' }))
      .toBeVisible()
    expect(
      document.querySelector('[data-testid="insights-skeleton"]')
    ).toBeNull()
  })

  it('overview 403 时仍走权限说明态而非错误块', async () => {
    vi.mocked(fetchInsightsOverview).mockRejectedValue(
      new ApiError('无权限', 403, 'forbidden')
    )
    const screen = await renderPage()

    await expect
      .element(screen.getByText('健康洞察仅工作区管理员可见'))
      .toBeVisible()
    expect(
      document.querySelector('[data-testid="insights-panel-error"]')
    ).toBeNull()
  })

  it('零召回查询失败时渲染错误块而不是「好状态」空态', async () => {
    vi.mocked(fetchZeroRecallQueries).mockRejectedValue(new Error('boom'))
    const screen = await renderPage()

    await expect
      .element(screen.getByTestId('insights-panel-error'))
      .toBeVisible()
    await expect.element(screen.getByText('加载失败')).toBeVisible()
    expect(document.body.textContent).not.toContain('近 14 天没有零召回查询')
    // 其余面板不受影响：成功且为真空数据时仍渲染各自空态。
    await expect.element(screen.getByText('没有沉睡内容')).toBeVisible()
    await expect.element(screen.getByText('最近没有内容变更')).toBeVisible()
  })

  it('错误块的重试按钮触发对应查询重新请求，成功后回到空态', async () => {
    let calls = 0
    vi.mocked(fetchZeroRecallQueries).mockImplementation(async () => {
      calls += 1
      if (calls === 1) throw new Error('boom')
      return emptyZeroRecall
    })
    const screen = await renderPage()

    await expect
      .element(screen.getByTestId('insights-panel-error'))
      .toBeVisible()
    await userEvent.click(screen.getByRole('button', { name: '重试' }))
    await vi.waitFor(() => expect(calls).toBe(2))
    await expect
      .element(screen.getByText('近 14 天没有零召回查询'))
      .toBeVisible()
  })
})
