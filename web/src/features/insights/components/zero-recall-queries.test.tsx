import type * as React from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

// Link 的最小 <a> 替身（项目既有惯例，见 web-source-card/knowledge-base-form 测试）：
// 替换 $params 并把 search 拼为 querystring，保持 href 断言语义。
vi.mock('@tanstack/react-router', () => ({
  Link: ({
    children,
    to,
    params,
    search,
    ...props
  }: React.ComponentProps<'a'> & {
    to: string
    params?: Record<string, string>
    search?: Record<string, string | undefined>
  }) => (
    <a
      href={
        Object.entries(params ?? {}).reduce<string>(
          (acc, [key, value]) => acc.replace(`$${key}`, value),
          to
        ) +
        (search
          ? `?${new URLSearchParams(
              Object.entries(search).filter(([, v]) => v !== undefined) as [
                string,
                string,
              ][]
            ).toString()}`
          : '')
      }
      {...props}
    >
      {children}
    </a>
  ),
}))

import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import type { ZeroRecallQuery } from '../types'
import { ZeroRecallQueries } from './zero-recall-queries'

const kbId = '20000000-0000-4000-8000-000000000002'

const enabledQueries: ZeroRecallQuery[] = [
  {
    query_hash: 'hash-1',
    query_chars: 8,
    hits: 12,
    first_seen_at: '2026-10-20T08:00:00Z',
    last_seen_at: '2026-10-27T09:14:00Z',
    latest_query_text: '发票抬头怎么改',
  },
  {
    query_hash: 'hash-2',
    query_chars: 13,
    hits: 8,
    first_seen_at: '2026-10-21T08:00:00Z',
    last_seen_at: '2026-10-26T13:02:00Z',
    latest_query_text: 'api key 轮换步骤',
  },
]

const maskedQueries: ZeroRecallQuery[] = [
  {
    query_hash: 'hash-1',
    query_chars: 8,
    hits: 12,
    first_seen_at: '2026-10-20T08:00:00Z',
    last_seen_at: '2026-10-27T09:14:00Z',
  },
]

const onEnableQueryLog = vi.fn<() => Promise<boolean>>()

function renderPanel(overrides: Record<string, unknown> = {}) {
  return render(
    <ZeroRecallQueries
      workspaceSlug='acme'
      kbId={kbId}
      windowDays={14}
      queryLogEnabled
      queries={enabledQueries}
      onEnableQueryLog={onEnableQueryLog}
      {...overrides}
    />
  )
}

describe('ZeroRecallQueries', () => {
  beforeEach(() => {
    onEnableQueryLog.mockReset()
  })

  it('开启原文记录时展示明文与行动按钮（创建 FAQ / 检索测试 深链）', async () => {
    const screen = await renderPanel()

    await expect.element(screen.getByText('发票抬头怎么改')).toBeVisible()
    await expect.element(screen.getByText('api key 轮换步骤')).toBeVisible()

    await expect
      .element(screen.getByRole('link', { name: '创建 FAQ' }).first())
      .toHaveAttribute(
        'href',
        `/workspaces/acme/kb/${kbId}/content/faq/new?question=${encodeURIComponent('发票抬头怎么改')}`
      )
    await expect
      .element(screen.getByRole('link', { name: '检索测试' }).first())
      .toHaveAttribute(
        'href',
        `/workspaces/acme/kb/${kbId}/search?q=${encodeURIComponent('发票抬头怎么改')}`
      )
  })

  it('未开启记录时只展示脱敏行与提示条，不出现明文与行动按钮', async () => {
    const screen = await renderPanel({
      queryLogEnabled: false,
      queries: maskedQueries,
    })

    await expect.element(screen.getByText('未开启查询原文记录')).toBeVisible()
    await expect.element(screen.getByText('•••（8 字）')).toBeVisible()
    expect(document.body.textContent).not.toContain('发票抬头怎么改')
    expect(document.querySelector('a[href*="/content/faq/new"]')).toBeNull()
    expect(document.querySelector('a[href*="/search?q="]')).toBeNull()
  })

  it('开启记录需二次确认：弹窗说明保留期与权限，确认成功后关闭', async () => {
    onEnableQueryLog.mockResolvedValue(true)
    const screen = await renderPanel({
      queryLogEnabled: false,
      queries: maskedQueries,
    })

    await userEvent.click(screen.getByRole('button', { name: '开启记录' }))
    const dialog = screen.getByRole('dialog')
    await expect.element(dialog.getByText('开启查询原文记录')).toBeVisible()
    await expect
      .element(dialog.getByText(/将保留 30 天，仅工作区管理员可读/))
      .toBeVisible()

    await userEvent.click(dialog.getByRole('button', { name: '开启记录' }))
    expect(onEnableQueryLog).toHaveBeenCalledTimes(1)
    await expect.element(screen.getByRole('dialog')).not.toBeInTheDocument()
  })

  it('确认回调失败时保持弹窗打开', async () => {
    onEnableQueryLog.mockResolvedValue(false)
    const screen = await renderPanel({
      queryLogEnabled: false,
      queries: maskedQueries,
    })

    await userEvent.click(screen.getByRole('button', { name: '开启记录' }))
    await userEvent.click(
      screen.getByRole('dialog').getByRole('button', { name: '开启记录' })
    )
    expect(onEnableQueryLog).toHaveBeenCalledTimes(1)
    await expect.element(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('开启记录但无原文的行操作列仍渲染（列数与表头一致），按钮置灰禁用', async () => {
    const screen = await renderPanel({
      queries: [
        {
          query_hash: 'hash-3',
          query_chars: 6,
          hits: 3,
          first_seen_at: '2026-10-22T08:00:00Z',
          last_seen_at: '2026-10-26T09:14:00Z',
        },
      ],
    })

    // 开启态表头 4 列（query/hits/lastSeen/actions）；无原文行不得缺操作列导致错位。
    const rows = document.querySelectorAll('tbody tr')
    expect(rows).toHaveLength(1)
    const headerCells = document.querySelectorAll('thead th')
    expect(headerCells).toHaveLength(4)
    expect(rows[0].querySelectorAll('td')).toHaveLength(headerCells.length)
    expect(rows[0].querySelector('td:last-child')?.textContent).toContain(
      '无原文'
    )

    const disabledButton = screen.getByRole('button', { name: '无原文' })
    await expect.element(disabledButton).toBeVisible()
    await expect.element(disabledButton).toBeDisabled()
    await expect
      .element(disabledButton)
      .toHaveAttribute('title', '开启记录前的查询无原文')
    expect(document.querySelector('a[href*="/content/faq/new"]')).toBeNull()
    expect(document.querySelector('a[href*="/search?q="]')).toBeNull()
  })

  it('零召回为空是好状态：开/关两态都渲染空态（关闭态仍保留开启入口）', async () => {
    const screen = await renderPanel({
      queryLogEnabled: false,
      queries: [],
    })

    await expect
      .element(screen.getByText('近 14 天没有零召回查询'))
      .toBeVisible()
    await expect
      .element(screen.getByRole('button', { name: '开启记录' }))
      .toBeVisible()
  })
})
