import { queryOptions } from '@tanstack/react-query'
import { getBootstrapStatus, getMe, getPublicInvitation } from './api'

export function meQueryOptions() {
  return queryOptions({
    queryKey: ['me'],
    queryFn: getMe,
    staleTime: 30_000,
  })
}

export function bootstrapStatusQueryOptions() {
  return queryOptions({
    queryKey: ['bootstrap-status'],
    queryFn: getBootstrapStatus,
    staleTime: 10_000,
  })
}

export function publicInvitationQueryOptions(token: string) {
  return queryOptions({
    queryKey: ['public-invitation', token],
    queryFn: () => getPublicInvitation(token),
    staleTime: 30_000,
  })
}
