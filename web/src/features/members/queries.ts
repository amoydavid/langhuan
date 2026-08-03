import { queryOptions } from '@tanstack/react-query'
import { listMembers } from './api'

export function membersQueryOptions(workspaceSlug: string) {
  return queryOptions({
    queryKey: ['members', workspaceSlug],
    queryFn: () => listMembers(workspaceSlug),
    staleTime: 15_000,
  })
}
