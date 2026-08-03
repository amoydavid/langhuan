import { apiClient } from '@/lib/api/client'
import type {
  CreateInvitationInput,
  CreateInvitationResponse,
  InvitationListItem,
} from './types'

function invitationsPath(workspaceSlug: string) {
  return `/workspaces/${encodeURIComponent(workspaceSlug)}/invitations`
}

export async function listInvitations(workspaceSlug: string) {
  const response = await apiClient.get<InvitationListItem[]>(
    invitationsPath(workspaceSlug)
  )
  return response.data
}

export async function createInvitation(
  workspaceSlug: string,
  input: CreateInvitationInput
) {
  const response = await apiClient.post<CreateInvitationResponse>(
    invitationsPath(workspaceSlug),
    input
  )
  return response.data
}

export async function revokeInvitation(
  workspaceSlug: string,
  invitationId: string
) {
  await apiClient.delete(
    `${invitationsPath(workspaceSlug)}/${encodeURIComponent(invitationId)}`
  )
}

export async function revokeInvitationAsPlatformAdmin(invitationId: string) {
  await apiClient.delete(`/invitations/${encodeURIComponent(invitationId)}`)
}
