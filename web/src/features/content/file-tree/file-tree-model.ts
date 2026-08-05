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
