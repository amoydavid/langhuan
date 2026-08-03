import { queryOptions } from '@tanstack/react-query'
import { listInvitations } from './api'

export function invitationsQueryOptions(workspaceSlug: string) {
  return queryOptions({
    queryKey: ['invitations', workspaceSlug],
    queryFn: () => listInvitations(workspaceSlug),
    staleTime: 10_000,
  })
}
