import { queryOptions } from '@tanstack/react-query'
import { getWorkspace } from './api'

export function workspaceQueryOptions(slug: string) {
  return queryOptions({
    queryKey: ['workspace', slug],
    queryFn: () => getWorkspace(slug),
    staleTime: 30_000,
  })
}
