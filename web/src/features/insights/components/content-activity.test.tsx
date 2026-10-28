import type * as React from 'react'
import { describe, expect, it, vi } from 'vitest'

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

import { render } from 'vitest-browser-react'
import type { InsightsActivityEvent } from '../types'
import {
  activityEventHref,
  activityEventTitle,
  ContentActivity,
} from './content-activity'

const kbId = '20000000-0000-4000-8000-000000000002'

function makeEvent(
  overrides: Partial<InsightsActivityEvent> = {}
): InsightsActivityEvent {
  return {
    kind: 'sync',
    target_document_id: null,
    source_id: null,
    source_name: null,
    document_kind: null,
    document_title: null,
    revision_reason: null,
    success: true,
    error_message: null,
    occurred_at: '2026-10-27T08:00:00Z',
    ...overrides,
  }
}

describe('activityEventTitle 文案组装', () => {
  it('同步事件按源名 + 成功/失败组装', () => {
    expect(activityEventTitle(makeEvent({ source_name: '产品文档站' }))).toBe(
      '产品文档站 同步成功'
    )
    expect(
      activityEventTitle(
        makeEvent({ source_name: '产品文档站', success: false })
      )
    ).toBe('产品文档站 同步失败')
  })

  it('revision 按 reason 映射动作文案，未知 reason 用兜底', () => {
    const cases: [string, string][] = [
      ['ingest', '安装指南 上传了新版本'],
      ['replace', '安装指南 上传了新版本'],
      ['crawl', '退款政策 抓取更新'],
      ['edit', '退款政策 编辑了内容'],
      ['reparse', '退款政策 重新解析'],
      ['mystery', '退款政策 更新了内容'],
    ]
    for (const [reason, expected] of cases) {
      expect(
        activityEventTitle(
          makeEvent({
            kind: 'revision',
            document_title:
              reason === 'ingest' || reason === 'replace'
                ? '安装指南'
                : '退款政策',
            revision_reason: reason,
          })
        )
      ).toBe(expected)
    }
  })

  it('delete 事件固定为移出检索文案', () => {
    expect(
      activityEventTitle(
        makeEvent({ kind: 'delete', document_title: '2024 报销制度（旧版）' })
      )
    ).toBe('2024 报销制度（旧版） 已移出检索')
  })
})

describe('activityEventHref 跳转', () => {
  it('同步行跳 web 源列表，文档事件跳详情，缺身份回内容列表', () => {
    const base = `/workspaces/acme/kb/${kbId}`
    expect(activityEventHref(makeEvent(), 'acme', kbId)).toBe(
      `${base}/content/web`
    )
    expect(
      activityEventHref(
        makeEvent({
          kind: 'revision',
          target_document_id: '30000000-0000-4000-8000-000000000003',
          document_kind: 'file',
        }),
        'acme',
        kbId
      )
    ).toBe(`${base}/content/files/30000000-0000-4000-8000-000000000003`)
    expect(
      activityEventHref(
        makeEvent({
          kind: 'delete',
          target_document_id: '50000000-0000-4000-8000-000000000005',
          document_kind: 'web',
        }),
        'acme',
        kbId
      )
    ).toBe(`${base}/content/web/50000000-0000-4000-8000-000000000005`)
    expect(activityEventHref(makeEvent({ kind: 'delete' }), 'acme', kbId)).toBe(
      `${base}/content/all`
    )
  })
})

describe('ContentActivity', () => {
  it('渲染事件行：文案、徽章与失败错误信息', async () => {
    const events = [
      makeEvent({ source_name: '产品文档站' }),
      makeEvent({
        kind: 'revision',
        document_title: '安装指南',
        revision_reason: 'ingest',
        occurred_at: '2026-10-26T18:32:00Z',
      }),
      makeEvent({
        source_name: '竞品帮助中心',
        success: false,
        error_message: '上游 503',
        occurred_at: '2026-10-25T22:00:00Z',
      }),
    ]
    const screen = await render(
      <ContentActivity
        workspaceSlug='acme'
        kbId={kbId}
        events={events}
        limit={20}
      />
    )

    await expect.element(screen.getByText('产品文档站 同步成功')).toBeVisible()
    await expect
      .element(screen.getByText('安装指南 上传了新版本'))
      .toBeVisible()
    await expect
      .element(screen.getByText('竞品帮助中心 同步失败'))
      .toBeVisible()
    await expect.element(screen.getByText('上游 503')).toBeVisible()

    // 失败同步行仍可点击；行整体是链接，失败信息随行展示。
    const links = document.querySelectorAll('a[href]')
    expect(links.length).toBe(3)
    expect(links[0]?.getAttribute('href')).toBe(
      `/workspaces/acme/kb/${kbId}/content/web`
    )
  })

  it('空事件显示空态文案', async () => {
    const screen = await render(
      <ContentActivity
        workspaceSlug='acme'
        kbId={kbId}
        events={[]}
        limit={20}
      />
    )

    await expect.element(screen.getByText('最近没有内容变更')).toBeVisible()
  })
})
