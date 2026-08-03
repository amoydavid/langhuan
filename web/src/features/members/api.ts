import type { Role } from '@/features/auth/types'
import { apiClient } from '@/lib/api/client'
import type { Member } from './types'

function membersPath(workspaceSlug: string) {
  return `/workspaces/${encodeURIComponent(workspaceSlug)}/members`
}

export async function listMembers(workspaceSlug: string) {
  const response = await apiClient.get<Member[]>(membersPath(workspaceSlug))
  return response.data
}

export async function changeMemberRole(
  workspaceSlug: string,
  userId: string,
  role: Role
) {
  const response = await apiClient.patch<Member>(
    `${membersPath(workspaceSlug)}/${encodeURIComponent(userId)}`,
    { role }
  )
  return response.data
}

export async function removeMember(workspaceSlug: string, userId: string) {
  await apiClient.delete(
    `${membersPath(workspaceSlug)}/${encodeURIComponent(userId)}`
  )
}

export async function resetUserPassword(userId: string, newPassword: string) {
  await apiClient.post(
    `/admin/users/${encodeURIComponent(userId)}/password-reset`,
    { new_password: newPassword }
  )
}
