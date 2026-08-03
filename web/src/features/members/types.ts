import type { Role } from '@/features/auth/types'

export type MemberUser = {
  email: string
  nickname: string
}

export type Member = {
  id: string
  workspace_id: string
  user_id: string
  role: Role
  user: MemberUser | null
  created_at: string
  updated_at: string
}
