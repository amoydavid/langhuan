import type { Document } from '@/features/documents/types'
import type { FileTreeNode } from './file-tree/schemas'

export function canonicalDocumentHref(workspaceSlug: string, item: Document) {
  const segment = item.kind === 'file' ? 'files' : item.kind
  return `/workspaces/${encodeURIComponent(workspaceSlug)}/kb/${encodeURIComponent(item.knowledge_base_id)}/content/${segment}/${encodeURIComponent(item.id)}`
}

export function findFileNode(
  node: FileTreeNode,
  documentId: string
): FileTreeNode | undefined {
  if (node.node_type === 'file' && node.document_id === documentId) return node
  for (const child of node.children) {
    const match = findFileNode(child, documentId)
    if (match) return match
  }
  return undefined
}
