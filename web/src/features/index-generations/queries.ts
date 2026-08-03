import { queryOptions } from '@tanstack/react-query'
import { listIndexGenerations } from './api'

export function indexGenerationsQueryOptions(
  workspaceSlug: string,
  kbId: string
) {
  return queryOptions({
    queryKey: ['index-generations', workspaceSlug, kbId],
    queryFn: () => listIndexGenerations(workspaceSlug, kbId),
    staleTime: 0,
    refetchInterval: (query) =>
      query.state.data?.some((item) => item.status === 'building')
        ? 2_000
        : false,
  })
}
