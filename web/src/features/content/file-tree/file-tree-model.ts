import type { FileTreeNode } from './schemas'

export type VisibleNode = { node: FileTreeNode; level: number }

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
    result.push({ node, level })
    // file 节点通常不可展开；web 源的「目录索引页」例外——其子页面挂在
    // 索引页节点下（v1.3.0），展开状态时随树显示。
    const expandable =
      node.node_type === 'root' ||
      node.node_type === 'folder' ||
      node.children.length > 0
    if (expandable && expanded.has(node.id)) {
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
