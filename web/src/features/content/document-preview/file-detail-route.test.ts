import { QueryClient, queryOptions } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'

const fixtures = vi.hoisted(() => ({
  document: {
    id: '30000000-0000-4000-8000-000000000003',
    workspace_id: '10000000-0000-4000-8000-000000000001',
    knowledge_base_id: '20000000-0000-4000-8000-000000000002',
    kind: 'file' as const,
    title: 'guide.docx',
    source_type: 'upload',
    source_uri: null,
    status: 'pending' as const,
    normalized_markdown: '',
    metadata: {},
    error_message: '',
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
  },
  chunks: vi.fn(),
}))

vi.mock('@/features/documents/queries', () => ({
  documentQueryOptions: () =>
    queryOptions({
      queryKey: ['document', fixtures.document.id],
      queryFn: async () => fixtures.document,
    }),
}))

vi.mock('@/features/content/file-tree/queries', () => ({
  fileTreeQueryOptions: () =>
    queryOptions({
      queryKey: ['file-tree'],
      queryFn: async () => ({
        root: {
          id: '60000000-0000-4000-8000-000000000006',
          parent_id: null,
          node_type: 'root' as const,
          name: 'root',
          document_id: null,
          path: '/',
          children: [],
        },
      }),
    }),
}))

vi.mock('@/features/knowledge-bases/workbench/queries', () => ({
  knowledgeBaseSummaryQueryOptions: () =>
    queryOptions({
      queryKey: ['knowledge-base-summary'],
      queryFn: async () => ({
        active_generation: {
          id: '70000000-0000-4000-8000-000000000007',
        },
      }),
    }),
}))

vi.mock('@/features/chunks/queries', () => ({
  chunkRevisionsQueryOptions: () =>
    queryOptions({
      queryKey: ['chunk-revisions'],
      queryFn: async () => [],
    }),
  documentChunksQueryOptions: () =>
    queryOptions({
      queryKey: ['document-chunks'],
      queryFn: fixtures.chunks,
    }),
}))

import { Route } from '@/routes/_authenticated/workspaces/$workspaceSlug/kb/$kbId/content/files/$documentId'

describe('file detail route loader', () => {
  it('does not request chunks while an uploaded document is still pending', async () => {
    const loader = Route.options.loader
    if (typeof loader !== 'function') {
      throw new Error('File detail route 缺少 callable loader')
    }
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })

    await loader({
      context: { queryClient },
      params: {
        workspaceSlug: 'acme',
        kbId: fixtures.document.knowledge_base_id,
        documentId: fixtures.document.id,
      },
      deps: { job: '80000000-0000-4000-8000-000000000008' },
    } as unknown as Parameters<typeof loader>[0])

    expect(fixtures.chunks).not.toHaveBeenCalled()
  })
})
