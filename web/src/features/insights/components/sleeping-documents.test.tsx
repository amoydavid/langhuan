import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-react'
import type { SleepingDocument } from '../types'
import { SleepingDocuments } from './sleeping-documents'

const kbId = '20000000-0000-4000-8000-000000000002'

const documents: SleepingDocument[] = [
  {
    document_id: '30000000-0000-4000-8000-000000000003',
    kind: 'file',
    title: '2024 报销制度（旧版）',
    indexed_at: '2025-11-02T08:00:00Z',
  },
  {
    document_id: '40000000-0000-4000-8000-000000000004',
    kind: 'faq',
    title: '内测口径备忘',
    indexed_at: '2026-01-09T08:00:00Z',
  },
  {
    document_id: '50000000-0000-4000-8000-000000000005',
    kind: 'web',
    title: 'onboarding-getting-started',
    indexed_at: '2025-12-18T08:00:00Z',
  },
]

function renderPanel(
  insufficientSample: boolean,
  docs: SleepingDocument[] = documents
) {
  return render(
    <SleepingDocuments
      workspaceSlug='acme'
      kbId={kbId}
      insufficientSample={insufficientSample}
      documents={docs}
    />
  )
}

describe('SleepingDocuments', () => {
  it('样本不足时只出提示块，不出表格与查看入口', async () => {
    const screen = await renderPanel(true)

    await expect.element(screen.getByText('检索样本不足')).toBeVisible()
    await expect
      .element(screen.getByText(/近 30 天完成检索少于 10 次/))
      .toBeVisible()
    expect(document.querySelector('table')).toBeNull()
    expect(document.querySelector('a[href*="/content/"]')).toBeNull()
  })

  it('正常出表：类型列、固定「可检索」徽章与按 kind 的详情深链', async () => {
    const screen = await renderPanel(false)

    await expect
      .element(screen.getByText('2024 报销制度（旧版）'))
      .toBeVisible()
    await expect
      .element(screen.getByText('onboarding-getting-started'))
      .toBeVisible()
    // 状态固定为可检索（沉睡内容仍在检索范围内，只是零引用）。
    const badges = document.querySelectorAll('[data-slot="status-badge"]')
    expect(badges.length).toBe(3)
    for (const badge of badges) {
      expect(badge.getAttribute('data-tone')).toBe('success')
      expect(badge.textContent).toContain('可检索')
    }

    await expect
      .element(screen.getByRole('link', { name: '查看 2024 报销制度（旧版）' }))
      .toHaveAttribute(
        'href',
        `/workspaces/acme/kb/${kbId}/content/files/30000000-0000-4000-8000-000000000003`
      )
    await expect
      .element(screen.getByRole('link', { name: '查看 内测口径备忘' }))
      .toHaveAttribute(
        'href',
        `/workspaces/acme/kb/${kbId}/content/faq/40000000-0000-4000-8000-000000000004`
      )
    await expect
      .element(
        screen.getByRole('link', { name: '查看 onboarding-getting-started' })
      )
      .toHaveAttribute(
        'href',
        `/workspaces/acme/kb/${kbId}/content/web/50000000-0000-4000-8000-000000000005`
      )
  })

  it('样本充足且无沉睡内容时显示正向空态', async () => {
    const screen = await renderPanel(false, [])

    await expect.element(screen.getByText('没有沉睡内容')).toBeVisible()
    expect(document.querySelector('table')).toBeNull()
  })
})
