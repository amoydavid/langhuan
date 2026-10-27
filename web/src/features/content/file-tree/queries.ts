import { queryOptions, useQuery } from '@tanstack/react-query'
import { fileTreeQueryKey } from '@/features/knowledge-bases/workbench/queries'
import { getDocumentCitationSummary, getFileTree } from './api'

export function fileTreeQueryOptions(workspaceSlug: string, kbId: string) {
  return queryOptions({
    queryKey: fileTreeQueryKey(workspaceSlug, kbId),
    queryFn: () => getFileTree(workspaceSlug, kbId),
    staleTime: 0,
  })
}

/**
 * 删除预警引用摘要（v1.4.0 §9.3.5）：文件删除确认框打开时并行请求。
 * 预警是增强不是闸门——请求失败静默降级为现状对话框（不重试、不抛错到 UI）。
 */
export function useDocumentCitationSummary(params: {
  workspaceSlug: string | undefined
  kbId: string | undefined
  /** 文件节点关联的 document_id；目录节点为 null，不请求。 */
  documentId: string | null
  enabled: boolean
}) {
  const { workspaceSlug, kbId, documentId, enabled } = params
  return useQuery({
    queryKey: ['document-citation-summary', workspaceSlug, kbId, documentId],
    queryFn: async () => {
      if (!workspaceSlug || !kbId || !documentId) return null
      return getDocumentCitationSummary(workspaceSlug, kbId, documentId)
    },
    enabled:
      enabled &&
      workspaceSlug !== undefined &&
      kbId !== undefined &&
      documentId !== null,
    retry: false,
    staleTime: 0,
  })
}
