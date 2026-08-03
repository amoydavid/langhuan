import { queryOptions } from '@tanstack/react-query'
import { fileTreeQueryKey } from '@/features/knowledge-bases/workbench/queries'
import { getFileTree } from './api'

export function fileTreeQueryOptions(workspaceSlug: string, kbId: string) {
  return queryOptions({
    queryKey: fileTreeQueryKey(workspaceSlug, kbId),
    queryFn: () => getFileTree(workspaceSlug, kbId),
    staleTime: 0,
  })
}
