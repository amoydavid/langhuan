import type { QueryClient } from '@tanstack/react-query'

export async function invalidateGenerationExperience(
  queryClient: QueryClient,
  workspaceSlug: string,
  kbId: string
) {
  await Promise.all([
    queryClient.invalidateQueries({
      queryKey: ['index-generations', workspaceSlug, kbId],
    }),
    queryClient.invalidateQueries({
      queryKey: ['knowledge-base-summary', workspaceSlug, kbId],
    }),
    queryClient.invalidateQueries({
      queryKey: ['knowledge-base', workspaceSlug, kbId],
    }),
    queryClient.invalidateQueries({
      queryKey: ['retrieval-test', workspaceSlug, kbId],
    }),
  ])
}
