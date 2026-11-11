import type { Role } from '@/features/auth/types'

/** KBScope 成员的知识库可见范围（仅 member 角色受 restricted 约束）。 */
export type KBScope = 'all' | 'restricted'

export type MemberUser = {
  email: string
  nickname: string
}

export type Member = {
  id: string
  workspace_id: string
  user_id: string
  role: Role
  kb_scope: KBScope
  knowledge_base_ids: string[]
  user: MemberUser | null
  created_at: string
  updated_at: string
}

export type MemberKBGrantsInput = {
  kb_scope: KBScope
  knowledge_base_ids?: string[]
}
