import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import { getDocumentCitationSummary } from './api'
import { DeleteDialog } from './file-tree-actions'
import type { FileTreeNode } from './schemas'

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  getDocumentCitationSummary: vi.fn(),
}))

const mockedSummary = vi.mocked(getDocumentCitationSummary)

const fileNode: FileTreeNode = {
  id: '10000000-0000-4000-8000-000000000010',
  parent_id: '11000000-0000-4000-8000-000000000011',
  node_type: 'file',
  name: '2024 报销制度.pdf',
  document_id: '12000000-0000-4000-8000-000000000012',
  path: '/制度/2024/报销制度.pdf',
  children: [],
}

async function renderDeleteDialog() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const screen = await render(
    <QueryClientProvider client={client}>
      <DeleteDialog
        node={fileNode}
        open
        onOpenChange={vi.fn()}
        onDelete={vi.fn()}
        onDeleted={vi.fn()}
        onError={vi.fn()}
        workspaceSlug='acme'
        kbId='kb-1'
      />
    </QueryClientProvider>
  )
  return { screen, client }
}

async function waitForCitationQuery(
  client: QueryClient,
  status: 'success' | 'error'
) {
  await vi.waitFor(() =>
    expect(
      client.getQueryState([
        'document-citation-summary',
        'acme',
        'kb-1',
        fileNode.document_id,
      ])?.status
    ).toBe(status)
  )
}

describe('DeleteDialog 引用预警（v1.4.0 §9.3.5）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('shows the citation warning block when citation_count > 0', async () => {
    mockedSummary.mockResolvedValue({
      window_days: 30,
      citation_count: 23,
      last_cited_at: new Date('2026-10-25T08:00:00Z').toISOString(),
    })
    const { screen } = await renderDeleteDialog()

    await expect.element(screen.getByText('近期仍被检索引用')).toBeVisible()
    await expect
      .element(screen.getByText(/近 30 天该内容在 23 次检索中被返回/))
      .toBeVisible()
    await expect
      .element(screen.getByText(/删除后相关问题将失去依据/))
      .toBeVisible()
    expect(mockedSummary).toHaveBeenCalledWith(
      'acme',
      'kb-1',
      '12000000-0000-4000-8000-000000000012'
    )
  })

  it('keeps the dialog unchanged when citation_count is 0', async () => {
    mockedSummary.mockResolvedValue({
      window_days: 30,
      citation_count: 0,
      last_cited_at: null,
    })
    const { screen, client } = await renderDeleteDialog()

    await waitForCitationQuery(client, 'success')
    await expect
      .element(screen.getByTestId('delete-citation-warning'))
      .not.toBeInTheDocument()
    await expect
      .element(
        screen.getByText('文件删除后不可恢复，关联的分块和向量将一并移除。')
      )
      .toBeVisible()
  })

  it('silently degrades to the current dialog when the request fails', async () => {
    mockedSummary.mockRejectedValue(new Error('network down'))
    const { screen, client } = await renderDeleteDialog()

    await waitForCitationQuery(client, 'error')
    await expect
      .element(screen.getByTestId('delete-citation-warning'))
      .not.toBeInTheDocument()
    await expect
      .element(screen.getByRole('button', { name: '确认删除' }))
      .toBeVisible()
  })
})
