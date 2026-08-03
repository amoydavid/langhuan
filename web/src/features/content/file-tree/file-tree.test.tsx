import { describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import { ApiError } from '@/lib/api/error'
import { FileTree } from './file-tree'
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
    ],
  },
})

describe('FileTree', () => {
  it('rejects non-file-domain node types at the API boundary', () => {
    const invalid = structuredClone(tree)
    invalid.root.children[0]!.node_type = 'faq' as 'folder'
    expect(fileTreeSchema.safeParse(invalid).success).toBe(false)
  })

  it('supports arrow navigation, Enter selection and F2 rename', async () => {
    const onSelectDocument = vi.fn()
    const onRenameNode = vi.fn().mockResolvedValue(undefined)
    const screen = await render(
      <FileTree
        tree={tree}
        canManage
        onSelectDocument={onSelectDocument}
        onRenameNode={onRenameNode}
      />
    )

    const folder = screen.getByRole('treeitem', { name: 'docs' })
    await userEvent.click(folder)
    await userEvent.keyboard('{ArrowRight}{ArrowRight}{Enter}')
    expect(onSelectDocument).toHaveBeenCalledWith(
      '60000000-0000-4000-8000-000000000006'
    )

    const file = screen.getByRole('treeitem', { name: 'installation.md' })
    await userEvent.click(file)
    await userEvent.keyboard('{F2}')
    const input = screen.getByRole('textbox', {
      name: '重命名 installation.md',
    })
    await userEvent.clear(input)
    await userEvent.fill(input, 'setup.md')
    await userEvent.keyboard('{Enter}')
    await vi.waitFor(() =>
      expect(onRenameNode).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'installation.md' }),
        'setup.md'
      )
    )
  })

  it('offers keyboard-accessible create, move and delete actions without drag and drop', async () => {
    const onCreateFolder = vi.fn().mockResolvedValue(undefined)
    const onMoveNode = vi.fn().mockResolvedValue(undefined)
    const onDeleteNode = vi.fn().mockResolvedValue(undefined)
    const screen = await render(
      <FileTree
        tree={tree}
        canManage
        onSelectDocument={vi.fn()}
        onCreateFolder={onCreateFolder}
        onMoveNode={onMoveNode}
        onDeleteNode={onDeleteNode}
      />
    )

    await userEvent.click(screen.getByRole('treeitem', { name: 'docs' }))
    await userEvent.click(screen.getByRole('button', { name: '新建文件夹' }))
    await userEvent.fill(screen.getByLabelText('文件夹名称'), 'guides')
    await userEvent.click(screen.getByRole('button', { name: '创建' }))
    await vi.waitFor(() =>
      expect(onCreateFolder).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'docs' }),
        'guides'
      )
    )

    await userEvent.click(screen.getByRole('treeitem', { name: 'docs' }))
    await userEvent.click(
      screen.getByRole('treeitem', { name: 'installation.md' })
    )
    await userEvent.click(
      screen.getByRole('button', { name: '移动 installation.md' })
    )
    await expect
      .element(screen.getByRole('dialog', { name: '选择目标目录' }))
      .toBeVisible()
    await userEvent.click(screen.getByRole('button', { name: '移动到 /' }))
    await vi.waitFor(() =>
      expect(onMoveNode).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'installation.md' }),
        expect.objectContaining({ node_type: 'root' })
      )
    )

    await userEvent.click(
      screen.getByRole('button', { name: '删除 installation.md' })
    )
    await expect.element(screen.getByText('会从检索中移除')).toBeVisible()
    await userEvent.click(screen.getByRole('button', { name: '确认删除' }))
    await vi.waitFor(() =>
      expect(onDeleteNode).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'installation.md' })
      )
    )
  })

  it('keeps rename mode open and explains a same-name conflict', async () => {
    const onRenameNode = vi
      .fn()
      .mockRejectedValue(
        new ApiError('conflict', 409, 'file_tree_name_conflict')
      )
    const screen = await render(
      <FileTree
        tree={tree}
        canManage
        onSelectDocument={vi.fn()}
        onRenameNode={onRenameNode}
      />
    )

    await userEvent.click(
      screen.getByRole('treeitem', { name: 'installation.md' })
    )
    await userEvent.keyboard('{F2}')
    const input = screen.getByRole('textbox', {
      name: '重命名 installation.md',
    })
    await userEvent.clear(input)
    await userEvent.fill(input, 'existing.md')
    await userEvent.keyboard('{Enter}')

    await expect
      .element(screen.getByText('目标目录中已存在同名项目，请更换名称。'))
      .toBeVisible()
    await expect.element(input).toBeVisible()
  })

  it('explains why a non-empty folder cannot be deleted', async () => {
    const onDeleteNode = vi
      .fn()
      .mockRejectedValue(new ApiError('not empty', 409, 'file_tree_not_empty'))
    const screen = await render(
      <FileTree
        tree={tree}
        canManage
        onSelectDocument={vi.fn()}
        onDeleteNode={onDeleteNode}
      />
    )

    await userEvent.click(screen.getByRole('treeitem', { name: 'docs' }))
    await userEvent.click(screen.getByRole('button', { name: '删除 docs' }))
    await userEvent.click(screen.getByRole('button', { name: '确认删除' }))

    await expect
      .element(screen.getByText('目录中仍有内容，请先移动或删除其中内容。'))
      .toBeVisible()
  })

  it('filters File nodes by readable name while retaining their parent folders', async () => {
    const screen = await render(
      <FileTree tree={tree} onSelectDocument={vi.fn()} />
    )
    await userEvent.fill(screen.getByLabelText('搜索文件'), 'installation')
    await expect
      .element(screen.getByRole('treeitem', { name: 'docs' }))
      .toBeVisible()
    await expect
      .element(screen.getByRole('treeitem', { name: 'installation.md' }))
      .toBeVisible()
    await userEvent.clear(screen.getByLabelText('搜索文件'))
    await userEvent.fill(screen.getByLabelText('搜索文件'), 'missing')
    await expect.element(screen.getByText('没有匹配的文件')).toBeVisible()
  })
})
