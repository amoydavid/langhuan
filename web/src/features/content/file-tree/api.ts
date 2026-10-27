import { z } from 'zod'
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

/** 文档引用摘要（v1.4.0 §9.3.5 删除预警）：窗口期内该文档被检索返回的次数与最近一次时间。 */
export const documentCitationSummarySchema = z.object({
  window_days: z.number().int().positive(),
  citation_count: z.number().int().nonnegative(),
  last_cited_at: z.string().nullable().optional(),
})

export type DocumentCitationSummary = z.infer<
  typeof documentCitationSummarySchema
>

export async function getDocumentCitationSummary(
  workspaceSlug: string,
  kbId: string,
  documentId: string,
  windowDays = 30
): Promise<DocumentCitationSummary> {
  const response = await apiClient.get<unknown>(
    `/workspaces/${encodeURIComponent(workspaceSlug)}/knowledge-bases/${encodeURIComponent(kbId)}/documents/${encodeURIComponent(documentId)}/citation-summary`,
    { params: { window_days: windowDays } }
  )
  return documentCitationSummarySchema.parse(response.data)
}
