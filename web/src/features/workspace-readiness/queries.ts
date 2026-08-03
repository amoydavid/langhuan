import { queryOptions } from '@tanstack/react-query'
import { getWorkspaceReadiness } from './api'

export function workspaceReadinessQueryOptions(workspaceSlug: string) {
  return queryOptions({
    queryKey: ['workspace-readiness', workspaceSlug],
    queryFn: () => getWorkspaceReadiness(workspaceSlug),
    staleTime: 5_000,
  })
}
