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

  it('shows the root and folders only, then reports the selected folder', async () => {
    const onSelectFolder = vi.fn()
    const screen = await render(
      <FileTree tree={tree} onSelectFolder={onSelectFolder} />
    )

    const root = screen.getByRole('treeitem', { name: '文件' })
    await userEvent.click(root)

    expect(onSelectFolder).toHaveBeenCalledWith(
      expect.objectContaining({ node_type: 'root', name: '文件' })
    )
    await expect
      .element(screen.getByRole('treeitem', { name: 'docs' }))
      .toBeVisible()
    await expect
      .element(screen.getByRole('treeitem', { name: 'installation.md' }))
      .not.toBeInTheDocument()
  })

  it('supports folder keyboard navigation and opens rename in a dialog', async () => {
    const onSelectFolder = vi.fn()
    const onRenameNode = vi.fn().mockResolvedValue(undefined)
    const screen = await render(
      <FileTree
        tree={tree}
        canManage
        onSelectFolder={onSelectFolder}
        onRenameNode={onRenameNode}
      />
    )

    const root = screen.getByRole('treeitem', { name: '文件' })
    await userEvent.click(root)
    await userEvent.keyboard('{ArrowDown}{Enter}')
    expect(onSelectFolder).toHaveBeenLastCalledWith(
      expect.objectContaining({ name: 'docs' })
    )

    await userEvent.keyboard('{F2}')
    await expect
      .element(screen.getByRole('dialog', { name: '重命名' }))
      .toBeVisible()
    const input = screen.getByRole('textbox', { name: '重命名 docs' })
    await userEvent.clear(input)
    await userEvent.fill(input, 'guides')
    await userEvent.click(screen.getByRole('button', { name: '保存名称' }))
    await vi.waitFor(() =>
      expect(onRenameNode).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'docs' }),
        'guides'
      )
    )
  })

  it('uses dialogs for folder creation, movement and deletion', async () => {
    const onCreateFolder = vi.fn().mockResolvedValue(undefined)
    const onMoveNode = vi.fn().mockResolvedValue(undefined)
    const onDeleteNode = vi.fn().mockResolvedValue(undefined)
    const screen = await render(
      <FileTree
        tree={tree}
        canManage
        onSelectFolder={vi.fn()}
        onCreateFolder={onCreateFolder}
        onMoveNode={onMoveNode}
        onDeleteNode={onDeleteNode}
      />
    )

    await userEvent.click(screen.getByRole('treeitem', { name: 'docs' }))
    await userEvent.click(screen.getByRole('button', { name: '新建文件夹' }))
    await expect
      .element(screen.getByRole('dialog', { name: '新建文件夹' }))
      .toBeVisible()
    await userEvent.fill(screen.getByLabelText('文件夹名称'), 'guides')
    await userEvent.click(screen.getByRole('button', { name: '创建' }))
    await vi.waitFor(() =>
      expect(onCreateFolder).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'docs' }),
        'guides'
      )
    )

    await userEvent.click(screen.getByRole('button', { name: 'docs 的操作' }))
    await userEvent.click(screen.getByRole('menuitem', { name: '移动' }))
    await expect
      .element(screen.getByRole('dialog', { name: '移动“docs”' }))
      .toBeVisible()
    await userEvent.click(screen.getByRole('button', { name: /文件/ }))
    await userEvent.click(screen.getByRole('button', { name: '移动到 /' }))
    await vi.waitFor(() =>
      expect(onMoveNode).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'docs' }),
        expect.objectContaining({ node_type: 'root' })
      )
    )

    await userEvent.click(screen.getByRole('button', { name: 'docs 的操作' }))
    await userEvent.click(screen.getByRole('menuitem', { name: '删除' }))
    await expect
      .element(screen.getByText('文件夹非空时无法删除，请先清空子内容。'))
      .toBeVisible()
    await userEvent.click(screen.getByRole('button', { name: '确认删除' }))
    await vi.waitFor(() =>
      expect(onDeleteNode).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'docs' })
      )
    )
  })

  it('keeps the rename dialog open and explains a same-name conflict', async () => {
    const onRenameNode = vi
      .fn()
      .mockRejectedValue(
        new ApiError('conflict', 409, 'file_tree_name_conflict')
      )
    const screen = await render(
      <FileTree
        tree={tree}
        canManage
        onSelectFolder={vi.fn()}
        onRenameNode={onRenameNode}
      />
    )

    await userEvent.click(screen.getByRole('treeitem', { name: 'docs' }))
    await userEvent.keyboard('{F2}')
    const input = screen.getByRole('textbox', { name: '重命名 docs' })
    await userEvent.clear(input)
    await userEvent.fill(input, 'existing')
    await userEvent.click(screen.getByRole('button', { name: '保存名称' }))

    await expect
      .element(screen.getByText('目标目录中已存在同名项目，请更换名称。'))
      .toBeVisible()
    await expect.element(input).toBeVisible()
  })

  it('filters folders by readable directory name without exposing files', async () => {
    const screen = await render(
      <FileTree tree={tree} onSelectFolder={vi.fn()} />
    )
    await userEvent.fill(screen.getByLabelText('搜索目录'), 'docs')
    await expect
      .element(screen.getByRole('treeitem', { name: 'docs' }))
      .toBeVisible()
    await expect
      .element(screen.getByRole('treeitem', { name: 'installation.md' }))
      .not.toBeInTheDocument()
    await userEvent.clear(screen.getByLabelText('搜索目录'))
    await userEvent.fill(screen.getByLabelText('搜索目录'), 'missing')
    await expect.element(screen.getByText('没有匹配的目录')).toBeVisible()
  })
})
