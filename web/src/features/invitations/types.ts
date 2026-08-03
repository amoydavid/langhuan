import type { Role } from '@/features/auth/types'

export type InvitationStatus = 'pending' | 'accepted' | 'expired' | 'revoked'

export type InvitationListItem = {
  id: string
  workspace_id: string
  invited_email: string
  role: Role
  token_prefix: string
  status: InvitationStatus
  expires_at: string
  accepted_at: string | null
  revoked_at: string | null
  created_by: string
  created_at: string
}

export type CreateInvitationInput = {
  invited_email: string
  role: Role
}

export type CreateInvitationResponse = {
  id: string
  invited_email: string
  role: Role
  expires_at: string
  token_prefix: string
  invite_url: string
}
