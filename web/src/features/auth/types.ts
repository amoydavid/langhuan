export type Role = 'owner' | 'admin' | 'member'

export type AuthenticatedUser = {
  id: string
  email: string
  nickname: string
  is_platform_admin: boolean
}

export type WorkspaceSummary = {
  workspace_id: string
  slug: string
  name: string
  role: Role
}

export type MeResponse = {
  user: AuthenticatedUser
  workspaces: WorkspaceSummary[]
}

export type BootstrapStatus = {
  initialized: boolean
}

export type PublicInvitation = {
  workspace_id: string
  workspace_name: string
  workspace_slug: string
  invited_email: string
  role: Role
  expires_at: string
}

export type LoginInput = {
  email: string
  password: string
}

export type LoginResponse = {
  user_id: string
}

export type RegisterInput = {
  email: string
  nickname: string
  password: string
  invitation_token?: string
}
