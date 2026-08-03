import { describe, expect, it } from 'vitest'
import type { Document } from '@/features/documents/types'
import type { FileTreeData } from './file-tree/schemas'
import { canonicalDocumentHref, findFileNode } from './routing'

const base: Omit<Document, 'id' | 'kind' | 'title'> = {
  workspace_id: '10000000-0000-4000-8000-000000000001',
  knowledge_base_id: '20000000-0000-4000-8000-000000000002',
  source_type: 'test',
  source_uri: null,
  status: 'ready',
  normalized_markdown: '',
  metadata: {},
  error_message: '',
  created_at: '2026-08-01T09:00:00Z',
  updated_at: '2026-08-01T10:00:00Z',
}

function item(kind: Document['kind']): Document {
  return {
    ...base,
    id: '30000000-0000-4000-8000-000000000003',
    kind,
    title: '可读名称',
  }
}

describe('content routing', () => {
  it.each([
    ['file', 'files'],
    ['faq', 'faq'],
    ['web', 'web'],
  ] as const)('builds the %s canonical route', (kind, segment) => {
    expect(canonicalDocumentHref('acme', item(kind))).toBe(
      `/workspaces/acme/kb/${base.knowledge_base_id}/content/${segment}/30000000-0000-4000-8000-000000000003`
    )
  })

  it('finds the File Tree display name and path without consulting Document.title', () => {
    const tree: FileTreeData = {
      workspace_id: base.workspace_id,
      knowledge_base_id: base.knowledge_base_id,
      root: {
        id: '40000000-0000-4000-8000-000000000004',
        parent_id: null,
        node_type: 'root',
        name: '文件',
        document_id: null,
        path: '/',
        children: [
          {
            id: '50000000-0000-4000-8000-000000000005',
            parent_id: '40000000-0000-4000-8000-000000000004',
            node_type: 'file',
            name: 'installation.md',
            document_id: item('file').id,
            path: '/docs/installation.md',
            children: [],
          },
        ],
      },
    }
    expect(findFileNode(tree.root, item('file').id)).toMatchObject({
      name: 'installation.md',
      path: '/docs/installation.md',
    })
  })
})
