import { describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import { ApiError } from '@/lib/api/error'
import { FAQForm } from './faq-form'
import type { FAQDocument } from './schemas'

const documentId = '30000000-0000-4000-8000-000000000003'
const baseRevisionId = '40000000-0000-4000-8000-000000000004'

function faqDocument(overrides: Partial<FAQDocument> = {}): FAQDocument {
  return {
    document: {
      id: documentId,
      workspace_id: '10000000-0000-4000-8000-000000000001',
      knowledge_base_id: '20000000-0000-4000-8000-000000000002',
      kind: 'faq',
      title: '退款政策',
      source_type: 'api',
      source_uri: null,
      status: 'ready',
      normalized_markdown: '',
      metadata: {},
      error_message: '',
      created_at: '2026-08-01T09:00:00Z',
      updated_at: '2026-08-01T10:00:00Z',
    },
    revision: {
      id: baseRevisionId,
      revision_no: 1,
      status: 'ready',
      created_at: '2026-08-01T10:00:00Z',
    },
    questions: ['如何退款？', '退款多久到账？'],
    answer: '请在订单页点击**申请退款**。',
    ...overrides,
  }
}

describe('FAQForm', () => {
  it('validates, adds, removes and keyboard-reorders question variants', async () => {
    const saveFAQ = vi.fn().mockResolvedValue(faqDocument())
    const screen = await render(<FAQForm mode='create' saveFAQ={saveFAQ} />)

    await userEvent.click(screen.getByRole('button', { name: '保存 FAQ' }))
    await expect.element(screen.getByText('请输入 FAQ 标题')).toBeVisible()
    await expect.element(screen.getByText('问题不能为空')).toBeVisible()
    await expect.element(screen.getByText('请输入回答')).toBeVisible()

    await userEvent.fill(screen.getByLabelText('标题'), '退款政策')
    await userEvent.fill(
      screen.getByRole('textbox', { name: '问题 1', exact: true }),
      '第一个问题'
    )
    await userEvent.click(screen.getByRole('button', { name: '添加问题' }))
    await userEvent.fill(
      screen.getByRole('textbox', { name: '问题 2', exact: true }),
      '第二个问题'
    )
    await userEvent.keyboard('{Alt>}{ArrowUp}{/Alt}')
    await expect
      .element(screen.getByRole('textbox', { name: '问题 1', exact: true }))
      .toHaveValue('第二个问题')
    await userEvent.click(screen.getByRole('button', { name: '删除问题 2' }))
    await expect
      .element(screen.getByRole('textbox', { name: '问题 2', exact: true }))
      .not.toBeInTheDocument()
  })

  it('renders sanitized Markdown preview and returns the created FAQ to canonical navigation', async () => {
    const created = faqDocument()
    const saveFAQ = vi.fn().mockResolvedValue(created)
    const onSaved = vi.fn()
    const screen = await render(
      <FAQForm mode='create' saveFAQ={saveFAQ} onSaved={onSaved} />
    )

    await userEvent.fill(screen.getByLabelText('标题'), '退款政策')
    await userEvent.fill(
      screen.getByRole('textbox', { name: '问题 1', exact: true }),
      '如何退款？'
    )
    await userEvent.fill(
      screen.getByRole('textbox', { name: '回答', exact: true }),
      '# 回答\n<script>alert(1)</script>\n[危险](javascript:alert(1))'
    )
    await userEvent.click(screen.getByRole('button', { name: '预览' }))
    await expect
      .element(screen.getByRole('heading', { name: '回答' }))
      .toBeVisible()
    const preview = document.querySelector('[data-testid="faq-answer-preview"]')
    expect(preview?.querySelector('script')).toBeNull()
    expect(preview?.querySelector('a')).toBeNull()
    await userEvent.click(screen.getByRole('button', { name: '保存 FAQ' }))

    await vi.waitFor(() =>
      expect(saveFAQ).toHaveBeenCalledWith({
        title: '退款政策',
        questions: ['如何退款？'],
        answer:
          '# 回答\n<script>alert(1)</script>\n[危险](javascript:alert(1))',
      })
    )
    expect(onSaved).toHaveBeenCalledWith(created)
  })

  it('always submits a base revision and preserves the RHF draft on conflict', async () => {
    const current = faqDocument()
    const latest = faqDocument({
      revision: {
        ...current.revision,
        id: '50000000-0000-4000-8000-000000000005',
        revision_no: 2,
      },
      questions: ['最新问题'],
      answer: '他人的最新回答',
    })
    const saveFAQ = vi
      .fn()
      .mockRejectedValueOnce(
        new ApiError('FAQ 已被他人更新', 409, 'revision_conflict')
      )
      .mockResolvedValueOnce(latest)
    const loadLatestFAQ = vi.fn().mockResolvedValue(latest)
    const screen = await render(
      <FAQForm
        mode='edit'
        initialFAQ={current}
        saveFAQ={saveFAQ}
        loadLatestFAQ={loadLatestFAQ}
      />
    )

    await userEvent.clear(
      screen.getByRole('textbox', { name: '问题 1', exact: true })
    )
    await userEvent.fill(
      screen.getByRole('textbox', { name: '问题 1', exact: true }),
      '我的问题'
    )
    await userEvent.clear(
      screen.getByRole('textbox', { name: '回答', exact: true })
    )
    await userEvent.fill(
      screen.getByRole('textbox', { name: '回答', exact: true }),
      '我的未保存回答'
    )
    await userEvent.click(screen.getByRole('button', { name: '保存新版本' }))

    await vi.waitFor(() =>
      expect(saveFAQ).toHaveBeenNthCalledWith(1, {
        base_revision_id: baseRevisionId,
        questions: ['我的问题', '退款多久到账？'],
        answer: '我的未保存回答',
      })
    )
    await expect.element(screen.getByText('FAQ 已被他人更新')).toBeVisible()
    await expect.element(screen.getByText('你的版本')).toBeVisible()
    await expect
      .element(screen.getByRole('heading', { name: '最新版本' }))
      .toBeVisible()
    await expect.element(screen.getByText('他人的最新回答')).toBeVisible()
    await expect
      .element(screen.getByRole('textbox', { name: '回答', exact: true }))
      .toHaveValue('我的未保存回答')

    await userEvent.click(
      screen.getByRole('button', { name: '基于最新版本重试' })
    )
    await vi.waitFor(() =>
      expect(saveFAQ).toHaveBeenLastCalledWith({
        base_revision_id: latest.revision.id,
        questions: ['我的问题', '退款多久到账？'],
        answer: '我的未保存回答',
      })
    )
  })
})
