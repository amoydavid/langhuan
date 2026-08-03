import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-react'
import type { Document } from '@/features/documents/types'
import { ContentList } from './content-list'

const workspaceId = '10000000-0000-4000-8000-000000000001'
const kbId = '20000000-0000-4000-8000-000000000002'

function makeDocument(
  id: string,
  kind: Document['kind'],
  title: string
): Document {
  return {
    id,
    workspace_id: workspaceId,
    knowledge_base_id: kbId,
    kind,
    title,
    source_type: kind === 'file' ? 'upload' : kind,
    source_uri: kind === 'web' ? 'https://example.com/guide' : null,
    status: 'ready',
    normalized_markdown: '',
    faq_question_count: kind === 'faq' ? 2 : undefined,
    metadata: kind === 'faq' ? { questions: ['如何退款？', '退款多久？'] } : {},
    error_message: '',
    created_at: '2026-08-01T09:00:00Z',
    updated_at: '2026-08-01T10:00:00Z',
  }
}

const documents = [
  makeDocument('30000000-0000-4000-8000-000000000003', 'file', '安装指南.md'),
  makeDocument('40000000-0000-4000-8000-000000000004', 'faq', '退款政策'),
  makeDocument('50000000-0000-4000-8000-000000000005', 'web', '产品帮助'),
]

describe('ContentList', () => {
  it('renders ordinary content as a desktop table and mobile cards without a file tree', async () => {
    const screen = await render(
      <ContentList
        workspaceSlug='acme'
        kbId={kbId}
        documents={documents}
        kind='all'
      />
    )

    await expect
      .element(screen.getByRole('table', { name: '全部内容' }))
      .toBeVisible()
    await expect
      .element(screen.getByTestId('content-cards'))
      .toBeInTheDocument()
    await expect.element(screen.getByText('安装指南.md').first()).toBeVisible()
    await expect.element(screen.getByText('退款政策').first()).toBeVisible()
    await expect.element(screen.getByText('产品帮助').first()).toBeVisible()
    await expect.element(screen.getByRole('tree')).not.toBeInTheDocument()
    expect(document.body.textContent).not.toContain(workspaceId)
    expect(document.body.textContent).not.toContain(kbId)
  })

  it('uses canonical per-kind detail links and exposes FAQ question counts', async () => {
    const screen = await render(
      <ContentList
        workspaceSlug='acme'
        kbId={kbId}
        documents={documents}
        kind='all'
      />
    )

    await expect
      .element(screen.getByRole('link', { name: '查看 安装指南.md' }).first())
      .toHaveAttribute(
        'href',
        `/workspaces/acme/kb/${kbId}/content/files/${documents[0]?.id}`
      )
    await expect.element(screen.getByText('2 个问题').first()).toBeVisible()
  })
})
