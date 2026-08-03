import { queryOptions } from '@tanstack/react-query'
import { getKnowledgeBase, listKnowledgeBases } from './api'

export function knowledgeBasesQueryOptions(workspaceSlug: string) {
  return queryOptions({
    queryKey: ['knowledge-bases', workspaceSlug],
    queryFn: () => listKnowledgeBases(workspaceSlug),
    staleTime: 15_000,
  })
}

export function knowledgeBaseQueryOptions(workspaceSlug: string, kbId: string) {
  return queryOptions({
    queryKey: ['knowledge-base', workspaceSlug, kbId],
    queryFn: () => getKnowledgeBase(workspaceSlug, kbId),
    staleTime: 15_000,
  })
}
