import type { Role } from '@/features/auth/types'

export function canManageContent(role: Role | undefined): boolean {
  return role !== undefined
}

export function canManageIndex(role: Role | undefined): boolean {
  return role === 'admin' || role === 'owner'
}

// 内容源写操作（web 源 CRUD/同步）服务端同样要求 admin/owner（RequireAdminForSession）。
export function canManageSources(role: Role | undefined): boolean {
  return role === 'admin' || role === 'owner'
}
