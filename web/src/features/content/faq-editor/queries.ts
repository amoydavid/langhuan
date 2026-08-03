import { type QueryClient, queryOptions } from '@tanstack/react-query'
import { getFAQDocument } from './api'
import type { FAQDocument } from './schemas'

export function faqDocumentQueryKey(workspaceSlug: string, documentId: string) {
  return ['faq-document', workspaceSlug, documentId] as const
}

export function faqDocumentQueryOptions(
  workspaceSlug: string,
  documentId: string
) {
  return queryOptions({
    queryKey: faqDocumentQueryKey(workspaceSlug, documentId),
    queryFn: () => getFAQDocument(workspaceSlug, documentId),
    staleTime: 0,
    refetchInterval: (query) => {
      const data = query.state.data as FAQDocument | undefined
      return data?.document.status === 'pending' ||
        data?.document.status === 'processing'
        ? 2_000
        : false
    },
  })
}

export async function invalidateFAQExperience(
  queryClient: QueryClient,
  workspaceSlug: string,
  kbId: string,
  documentId: string
) {
  await Promise.all([
    queryClient.invalidateQueries({
      queryKey: ['documents', workspaceSlug, kbId],
    }),
    queryClient.invalidateQueries({
      queryKey: ['document', workspaceSlug, documentId],
    }),
    queryClient.invalidateQueries({
      queryKey: ['knowledge-base-summary', workspaceSlug, kbId],
    }),
    queryClient.invalidateQueries({
      queryKey: ['document-chunks', workspaceSlug, kbId, documentId],
    }),
    queryClient.invalidateQueries({
      queryKey: ['index-generations', workspaceSlug, kbId],
    }),
    queryClient.invalidateQueries({
      queryKey: ['retrieval-test', workspaceSlug, kbId],
    }),
  ])
}
