import type { FileTreeNode } from './schemas'

export type VisibleNode = { node: FileTreeNode; level: number }

/**
 * 剪掉 web 文档（kind='web'）对应的文件节点及其子树。
 *
 * web 源页面在 file_tree_nodes 里也是 file 节点（v1.3.0），但「文件」tab
 * 只承载上传文件——web 页面由「Web」tab 管理。目录索引页（如 /blog/ 既是
 * 页面又是子页面的父）的子页面全部为 web 文档，会随索引页节点一起剪掉；
 * 剪后变为空的 folder 保留（与用户新建空文件夹的展示行为一致）。
 */
export function withoutWebDocuments(
  node: FileTreeNode,
  webDocumentIds: ReadonlySet<string>
): FileTreeNode {
  const children = node.children
    .map((child) => withoutWebDocuments(child, webDocumentIds))
    .filter(
      (child) =>
        !(
          child.node_type === 'file' &&
          child.document_id !== null &&
          webDocumentIds.has(child.document_id)
        )
    )
  return { ...node, children }
}

/**
 * 返回只包含根目录与文件夹的目录树；文件仅在右侧文件列表中出现。
 */
export function folderTree(node: FileTreeNode): FileTreeNode | undefined {
  if (node.node_type === 'file') return undefined
  return {
    ...node,
    children: node.children.flatMap((child) => {
      const folder = folderTree(child)
      return folder ? [folder] : []
    }),
  }
}

export function folderIds(node: FileTreeNode): string[] {
  return [
    ...(node.node_type === 'root' || node.node_type === 'folder'
      ? [node.id]
      : []),
    ...node.children.flatMap(folderIds),
  ]
}

export function visibleNodes(
  root: FileTreeNode,
  expanded: ReadonlySet<string>
): VisibleNode[] {
  const result: VisibleNode[] = []
  function visit(node: FileTreeNode, level: number) {
    if (node.node_type === 'file') return
    result.push({ node, level })
    if (
      (node.node_type === 'root' || node.node_type === 'folder') &&
      expanded.has(node.id)
    ) {
      for (const child of node.children) visit(child, level + 1)
    }
  }
  visit(root, 0)
  return result
}

export function filterTree(
  node: FileTreeNode,
  query: string
): FileTreeNode | undefined {
  if (node.node_type === 'file') return undefined
  if (!query) return folderTree(node)
  const children = node.children
    .map((child) => filterTree(child, query))
    .filter((child): child is FileTreeNode => Boolean(child))
  const matches = node.name.toLocaleLowerCase().includes(query)
  if (matches || children.length > 0) {
    return { ...node, children }
  }
  return undefined
}

export function findNode(
  node: FileTreeNode,
  id: string
): FileTreeNode | undefined {
  if (node.id === id) return node
  for (const child of node.children) {
    const match = findNode(child, id)
    if (match) return match
  }
  return undefined
}

export function allFolders(node: FileTreeNode): FileTreeNode[] {
  return [
    ...(node.node_type === 'root' || node.node_type === 'folder' ? [node] : []),
    ...node.children.flatMap(allFolders),
  ]
}

export function descendantIds(node: FileTreeNode): Set<string> {
  return new Set([
    node.id,
    ...node.children.flatMap((child) => [...descendantIds(child)]),
  ])
}
