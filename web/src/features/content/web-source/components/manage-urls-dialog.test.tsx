import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import { updateWebSource } from '../api'
import type { WebSourceView } from '../types'
import { ManageUrlsDialog } from './manage-urls-dialog'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  updateWebSource: vi.fn().mockResolvedValue({
    source: {},
    job_id: '70000000-0000-4000-8000-000000000007',
  }),
}))
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))
vi.mock('@tanstack/react-router', () => ({
  Link: ({ children }: { children: React.ReactNode }) => (
    <a href='#test'>{children}</a>
  ),
}))

const mockedUpdate = vi.mocked(updateWebSource)

const sourceId = '60000000-0000-4000-8000-000000000006'

function makeSource(): WebSourceView {
  return {
    source: {
      id: sourceId,
      workspace_id: '30000000-0000-4000-8000-000000000003',
      knowledge_base_id: '20000000-0000-4000-8000-000000000002',
      kind: 'web',
      name: '竞品帮助中心',
      status: 'active',
      config: {
        mode: 'urls',
        urls: [
          'https://example.com/help/getting-started',
          'https://example.com/help/faq',
          'https://example.com/pricing',
        ],
        on_delete: 'keep',
      },
      sync_cursor: {},
      next_sync_at: null,
      sync_requested_force: false,
      sync_last_result: {},
      created_at: '2026-09-01T00:00:00Z',
      updated_at: '2026-09-16T08:00:00Z',
    },
    page_count: 30,
    failed_page_count: 2,
  }
}

async function renderDialog(source = makeSource()) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const screen = await render(
    <QueryClientProvider client={client}>
      <ManageUrlsDialog
        workspaceSlug='acme'
        kbId='kb-1'
        source={source}
        open
        onOpenChange={vi.fn()}
      />
    </QueryClientProvider>
  )
  return { screen, source }
}

describe('ManageUrlsDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('lists current urls as checked entries', async () => {
    const { screen } = await renderDialog()
    await expect
      .element(
        screen.getByRole('checkbox', { name: 'https://example.com/help/faq' })
      )
      .toBeChecked()
    await expect.element(screen.getByText(/当前 3 个 URL/)).toBeVisible()
  })

  it('previews additions and removals before saving', async () => {
    const { screen } = await renderDialog()
    // 取消勾选一个 URL → 移除 1
    await screen
      .getByRole('checkbox', { name: 'https://example.com/pricing' })
      .click()
    await expect.element(screen.getByText(/移除 1/)).toBeVisible()

    // 粘贴一行新增 + 一行非法 → 新增 1，且提示无效行
    await screen
      .getByLabelText('新增（每行一个）')
      .fill('https://example.com/help/changelog\nnot-a-url')
    await expect.element(screen.getByText(/新增 1/)).toBeVisible()
    await expect.element(screen.getByText(/1 行无效将被拒绝/)).toBeVisible()
  })

  it('saves the whole replacement list and reports remaining urls', async () => {
    const onOpenChange = vi.fn()
    const client = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    const screen = await render(
      <QueryClientProvider client={client}>
        <ManageUrlsDialog
          workspaceSlug='acme'
          kbId='kb-1'
          source={makeSource()}
          open
          onOpenChange={onOpenChange}
        />
      </QueryClientProvider>
    )
    await screen
      .getByRole('checkbox', { name: 'https://example.com/pricing' })
      .click()
    await screen
      .getByLabelText('新增（每行一个）')
      .fill('https://example.com/help/changelog')

    await screen.getByRole('button', { name: '保存' }).click()

    await vi.waitFor(() => {
      expect(mockedUpdate).toHaveBeenCalledTimes(1)
    })
    expect(mockedUpdate).toHaveBeenCalledWith('acme', 'kb-1', sourceId, {
      config: {
        urls: [
          'https://example.com/help/getting-started',
          'https://example.com/help/faq',
          'https://example.com/help/changelog',
        ],
      },
    })
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('keeps the save button disabled when nothing changed', async () => {
    const { screen } = await renderDialog()
    await expect
      .element(screen.getByRole('button', { name: '保存' }))
      .toBeDisabled()
  })

  it('flags duplicate lines in the preview and excludes them from the payload', async () => {
    const { screen } = await renderDialog()
    await screen
      .getByLabelText('新增（每行一个）')
      .fill('https://example.com/pricing\nhttps://example.com/help/changelog')

    // pricing 已存在且未勾选移除 → 预览提示重复并忽略，仅计 1 行新增。
    await expect.element(screen.getByText(/新增 1 · 移除 0/)).toBeVisible()
    await expect.element(screen.getByText(/1 行与现有清单重复/)).toBeVisible()

    await screen.getByRole('button', { name: '保存' }).click()
    await vi.waitFor(() => {
      expect(mockedUpdate).toHaveBeenCalledTimes(1)
    })
    expect(mockedUpdate).toHaveBeenCalledWith('acme', 'kb-1', sourceId, {
      config: {
        urls: [
          'https://example.com/help/getting-started',
          'https://example.com/help/faq',
          'https://example.com/pricing',
          'https://example.com/help/changelog',
        ],
      },
    })
  })

  it('allows re-adding a URL that was unchecked for removal', async () => {
    const { screen } = await renderDialog()
    // 取消勾选 pricing（将移除），再通过新增行加回：合法变更，非重复。
    await screen
      .getByRole('checkbox', { name: 'https://example.com/pricing' })
      .click()
    await screen
      .getByLabelText('新增（每行一个）')
      .fill('https://example.com/pricing')

    await expect.element(screen.getByText(/新增 1 · 移除 1/)).toBeVisible()
    await expect
      .element(screen.getByText(/与现有清单重复/))
      .not.toBeInTheDocument()

    await screen.getByRole('button', { name: '保存' }).click()
    await vi.waitFor(() => {
      expect(mockedUpdate).toHaveBeenCalledTimes(1)
    })
    // pricing 在结果清单中只出现一次（保留位次不变，语义上为「未移除」）。
    expect(mockedUpdate).toHaveBeenCalledWith('acme', 'kb-1', sourceId, {
      config: {
        urls: [
          'https://example.com/help/getting-started',
          'https://example.com/help/faq',
          'https://example.com/pricing',
        ],
      },
    })
  })
})
