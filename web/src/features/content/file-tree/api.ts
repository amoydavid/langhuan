import { apiClient } from '@/lib/api/client'
import { fileTreeNodeSchema, fileTreeSchema } from './schemas'

function fileTreePath(workspaceSlug: string, kbId: string) {
  return `/workspaces/${encodeURIComponent(workspaceSlug)}/knowledge-bases/${encodeURIComponent(kbId)}/file-tree`
}

export async function getFileTree(workspaceSlug: string, kbId: string) {
  const response = await apiClient.get<unknown>(
    fileTreePath(workspaceSlug, kbId)
  )
  return fileTreeSchema.parse(response.data)
}

export async function createFileTreeFolder(
  workspaceSlug: string,
  kbId: string,
  input: { parent_id: string; name: string }
) {
  const response = await apiClient.post<unknown>(
    `${fileTreePath(workspaceSlug, kbId)}/folders`,
    input
  )
  return fileTreeNodeSchema.parse(response.data)
}

export async function updateFileTreeNode(
  workspaceSlug: string,
  kbId: string,
  nodeId: string,
  input: { name?: string; parent_id?: string }
) {
  await apiClient.patch(
    `${fileTreePath(workspaceSlug, kbId)}/nodes/${encodeURIComponent(nodeId)}`,
    input
  )
}

export async function deleteFileTreeNode(
  workspaceSlug: string,
  kbId: string,
  nodeId: string
) {
  await apiClient.delete(
    `${fileTreePath(workspaceSlug, kbId)}/nodes/${encodeURIComponent(nodeId)}`
  )
}
