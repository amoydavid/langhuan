import type { Role } from '@/features/auth/types'

export function canManageContent(role: Role | undefined): boolean {
  return role !== undefined
}

export function canManageIndex(role: Role | undefined): boolean {
  return role === 'admin' || role === 'owner'
}
