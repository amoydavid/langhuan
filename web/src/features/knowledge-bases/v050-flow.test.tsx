import { describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import { ChunkInspector } from '@/features/chunks/inspector/chunk-inspector'
import type { Chunk } from '@/features/chunks/types'
import { FileTree } from '@/features/content/file-tree/file-tree'
import { fileTreeSchema } from '@/features/content/file-tree/schemas'
import { GenerationList } from '@/features/index-generations/generation-list'
import { IndexWriteForbidden } from '@/features/index-generations/index-write-forbidden'
import type { IndexGeneration } from '@/features/index-generations/types'
import { canManageContent, canManageIndex } from './permissions'

const ids = {
  workspace: '10000000-0000-4000-8000-000000000001',
  knowledgeBase: '20000000-0000-4000-8000-000000000002',
  root: '30000000-0000-4000-8000-000000000003',
  folder: '35000000-0000-4000-8000-000000000003',
  file: '40000000-0000-4000-8000-000000000004',
  document: '50000000-0000-4000-8000-000000000005',
  chunk: '60000000-0000-4000-8000-000000000006',
  generation: '70000000-0000-4000-8000-000000000007',
}

const tree = fileTreeSchema.parse({
  workspace_id: ids.workspace,
  knowledge_base_id: ids.knowledgeBase,
  root: {
    id: ids.root,
    parent_id: null,
    node_type: 'root',
    name: '文件',
    document_id: null,
    path: '/',
    children: [
      {
        id: ids.folder,
        parent_id: ids.root,
        node_type: 'folder',
        name: 'docs',
        document_id: null,
        path: '/docs',
        children: [
          {
            id: ids.file,
            parent_id: ids.folder,
            node_type: 'file',
            name: 'installation.md',
            document_id: ids.document,
            path: '/docs/installation.md',
            children: [],
          },
        ],
      },
    ],
  },
})

const chunk: Chunk = {
  id: ids.chunk,
  workspace_id: ids.workspace,
  knowledge_base_id: ids.knowledgeBase,
  document_id: ids.document,
  document_revision_id: '80000000-0000-4000-8000-000000000008',
  chunk_set_id: '90000000-0000-4000-8000-000000000009',
  sequence: 1,
  source_content: '安装说明',
  source_anchor: { line_start: 1, line_end: 1 },
  metadata: {},
  active_revision: {
    id: 'a0000000-0000-4000-8000-00000000000a',
    chunk_id: ids.chunk,
    revision_no: 1,
    content: '安装说明',
    context_header: '安装',
    enabled: true,
    status: 'ready',
    edit_source: 'system',
    editor_display_name: '系统',
    error_message: '',
    created_at: '2026-08-01T10:00:00Z',
  },
  created_at: '2026-08-01T10:00:00Z',
}

const generation: IndexGeneration = {
  id: ids.generation,
  workspace_id: ids.workspace,
  knowledge_base_id: ids.knowledgeBase,
  embedding_model_id: 'b0000000-0000-4000-8000-00000000000b',
  provider_id: 'c0000000-0000-4000-8000-00000000000c',
  model_name: 'text-embedding-v4',
  display_label: '2026-08-01 11:08 · text-embedding-v4 · ready',
  embedding_dimension: 1024,
  chunker_version: 1,
  chunking_config: { chunk_size: 1000, chunk_overlap: 100 },
  retrieval_config: {
    fts_config: 'simple',
    vector_top_k: 20,
    keyword_top_k: 20,
    final_top_k: 8,
    rrf_k: 60,
  },
  config_hash: 'diagnostic-hash-must-not-render',
  source_content_version: 1,
  indexed_content_version: 1,
  status: 'ready',
  document_count: 1,
  chunk_count: 1,
  indexed_count: 1,
  manual_edit_count: 0,
  disabled_chunk_count: 0,
  manual_edit_disposition: 'not_applicable',
  error_class: '',
  error_message: '',
  created_at: '2026-08-01T11:08:00Z',
}

describe('v0.5.0 role boundary', () => {
  it('lets members manage content while keeping index mutations administrative', () => {
    expect(canManageContent('member')).toBe(true)
    expect(canManageContent('admin')).toBe(true)
    expect(canManageContent('owner')).toBe(true)
    expect(canManageContent(undefined)).toBe(false)

    expect(canManageIndex('member')).toBe(false)
    expect(canManageIndex('admin')).toBe(true)
    expect(canManageIndex('owner')).toBe(true)
    expect(canManageIndex(undefined)).toBe(false)
  })

  it('keeps member content controls available and Chunk/Generation controls read-only', async () => {
    const canManage = canManageContent('member')
    const fileTree = await render(
      <FileTree
        tree={tree}
        canManage={canManage}
        onSelectFolder={vi.fn()}
        onCreateFolder={vi.fn()}
        onRenameNode={vi.fn()}
        onMoveNode={vi.fn()}
        onDeleteNode={vi.fn()}
      />
    )
    await expect
      .element(fileTree.getByRole('button', { name: '新建文件夹' }))
      .toBeVisible()
    await userEvent.click(fileTree.getByRole('treeitem', { name: 'docs' }))
    // 行级 ⋯ 菜单在 hover/focus 时出现；先触发 hover 再打开菜单
    await userEvent.click(fileTree.getByRole('button', { name: 'docs 的操作' }))
    await expect
      .element(fileTree.getByRole('menuitem', { name: '删除' }))
      .toBeVisible()

    const chunkInspector = await render(
      <ChunkInspector
        documentTitle='installation.md'
        documentKind='file'
        chunks={[chunk]}
        selectedChunkId={ids.chunk}
        page={1}
        canEdit={canManageIndex('member')}
      />
    )
    await expect
      .element(chunkInspector.getByRole('button', { name: '编辑分块 1' }))
      .not.toBeInTheDocument()

    const generations = await render(
      <GenerationList
        generations={[generation]}
        activeGenerationId={ids.generation}
        canManage={canManageIndex('member')}
        activateGeneration={vi.fn()}
      />
    )
    await expect
      .element(generations.getByRole('button', { name: '比较并激活' }))
      .not.toBeInTheDocument()
    expect(document.body.textContent).not.toContain(ids.workspace)
    expect(document.body.textContent).not.toContain(ids.knowledgeBase)
    expect(document.body.textContent).not.toContain(
      'diagnostic-hash-must-not-render'
    )
  })

  it('shows an explicit 403 for a member deep-linking to Generation creation', async () => {
    const screen = await render(<IndexWriteForbidden onBack={vi.fn()} />)
    await expect.element(screen.getByText('403')).toBeVisible()
    await expect
      .element(screen.getByRole('heading', { name: '无权构建索引版本' }))
      .toBeVisible()
    await expect
      .element(screen.getByText('索引配置由 Workspace 管理员维护'))
      .toBeVisible()
  })
})
