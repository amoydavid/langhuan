import { describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import type { Document } from '@/features/documents/types'
import { FileBrowserList } from './file-browser-list'
import { fileTreeSchema } from './schemas'

const tree = fileTreeSchema.parse({
  workspace_id: '10000000-0000-4000-8000-000000000001',
  knowledge_base_id: '20000000-0000-4000-8000-000000000002',
  root: {
    id: '30000000-0000-4000-8000-000000000003',
    parent_id: null,
    node_type: 'root',
    name: '文件',
    document_id: null,
    path: '/',
    children: [
      {
        id: '40000000-0000-4000-8000-000000000004',
        parent_id: '30000000-0000-4000-8000-000000000003',
        node_type: 'folder',
        name: 'docs',
        document_id: null,
        path: '/docs',
        children: [
          {
            id: '50000000-0000-4000-8000-000000000005',
            parent_id: '40000000-0000-4000-8000-000000000004',
            node_type: 'file',
            name: 'installation.md',
            document_id: '60000000-0000-4000-8000-000000000006',
            path: '/docs/installation.md',
            children: [],
          },
        ],
      },
      {
        id: '70000000-0000-4000-8000-000000000007',
        parent_id: '30000000-0000-4000-8000-000000000003',
        node_type: 'file',
        name: 'root.md',
        document_id: '80000000-0000-4000-8000-000000000008',
        path: '/root.md',
        children: [],
      },
    ],
  },
})

const documents = [
  {
    id: '60000000-0000-4000-8000-000000000006',
    workspace_id: '10000000-0000-4000-8000-000000000001',
    knowledge_base_id: '20000000-0000-4000-8000-000000000002',
    kind: 'file',
    title: 'installation.md',
    source_type: 'upload',
    source_uri: null,
    status: 'ready',
    normalized_markdown: '',
    metadata: {},
    error_message: '',
    created_at: '2026-08-05T01:00:00Z',
    updated_at: '2026-08-05T02:00:00Z',
    active_revision: undefined,
  },
  {
    id: '80000000-0000-4000-8000-000000000008',
    workspace_id: '10000000-0000-4000-8000-000000000001',
    knowledge_base_id: '20000000-0000-4000-8000-000000000002',
    kind: 'file',
    title: 'root.md',
    source_type: 'upload',
    source_uri: null,
    status: 'processing',
    normalized_markdown: '',
    metadata: {},
    error_message: '',
    created_at: '2026-08-05T01:00:00Z',
    updated_at: '2026-08-05T03:00:00Z',
    active_revision: undefined,
  },
] satisfies Document[]

describe('FileBrowserList', () => {
  it('lists only files directly under the selected folder and opens one from its row', async () => {
    const onOpenFile = vi.fn()
    const screen = await render(
      <FileBrowserList
        folder={tree.root.children[0]!}
        documents={documents}
        onOpenFile={onOpenFile}
      />
    )

    await expect.element(screen.getByText('/docs')).toBeVisible()
    await expect.element(screen.getByText('installation.md')).toBeVisible()
    await expect.element(screen.getByText('root.md')).not.toBeInTheDocument()
    await userEvent.click(
      screen.getByRole('button', { name: 'installation.md', exact: true })
    )
    expect(onOpenFile).toHaveBeenCalledWith(
      '60000000-0000-4000-8000-000000000006'
    )
  })
})
