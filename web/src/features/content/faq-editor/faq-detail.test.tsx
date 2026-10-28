import type * as React from 'react'
import { describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'

// Link 的最小 <a> 替身（项目既有惯例，见 content-activity/web-source-card 测试）：
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
    search?: Record<string, string | number | undefined>
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

import { FAQDetail } from './faq-detail'
import type { FAQDocument } from './schemas'

const documentId = '30000000-0000-4000-8000-000000000003'
const revisionId = '40000000-0000-4000-8000-000000000004'

const faq: FAQDocument = {
  document: {
    id: documentId,
    workspace_id: '10000000-0000-4000-8000-000000000001',
    knowledge_base_id: '20000000-0000-4000-8000-000000000002',
    kind: 'faq',
    title: '退款政策',
    source_type: 'api',
    source_uri: null,
    status: 'processing',
    normalized_markdown: '',
    metadata: {},
    error_message: '',
    created_at: '2026-08-01T09:00:00Z',
    updated_at: '2026-08-01T10:00:00Z',
  },
  revision: {
    id: revisionId,
    revision_no: 2,
    status: 'ready',
    created_at: '2026-08-01T10:00:00Z',
  },
  questions: ['如何退款？', '退款多久到账？'],
  answer:
    '请在订单页点击**申请退款**。\n<script>alert(1)</script>\n[危险](javascript:alert(1))',
}

describe('FAQDetail', () => {
  it('shows readable questions, sanitized answer, processing feedback and edit action', async () => {
    const screen = await render(
      <FAQDetail faq={faq} canEdit workspaceSlug='acme' />
    )

    await expect
      .element(screen.getByRole('heading', { name: '退款政策' }))
      .toBeVisible()
    await expect.element(screen.getByText('如何退款？')).toBeVisible()
    await expect.element(screen.getByText('退款多久到账？')).toBeVisible()
    await expect.element(screen.getByText('新版本正在建立索引')).toBeVisible()
    await expect
      .element(screen.getByRole('link', { name: '编辑 FAQ' }))
      .toHaveAttribute(
        'href',
        `/workspaces/acme/kb/${faq.document.knowledge_base_id}/content/faq/${documentId}/edit`
      )
    const answer = document.querySelector('[data-testid="faq-answer"]')
    expect(answer?.querySelector('script')).toBeNull()
    expect(answer?.querySelector('a[href^="javascript:"]')).toBeNull()
    expect(document.body.textContent).not.toContain(documentId)
    expect(document.body.textContent).not.toContain(revisionId)
  })

  it('keeps the detail read-only when editing is not allowed', async () => {
    const screen = await render(
      <FAQDetail faq={faq} canEdit={false} workspaceSlug='acme' />
    )

    await expect
      .element(screen.getByRole('link', { name: '编辑 FAQ' }))
      .not.toBeInTheDocument()
  })
})
