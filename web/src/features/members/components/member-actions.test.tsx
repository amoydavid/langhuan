import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import { buildWorkspaceNavigation } from '@/components/layout/workspace-navigation'
import { ApiError } from '@/lib/api/error'
import type { Member } from '../types'
import { MemberActions, memberActionErrorMessage } from './member-actions'

vi.mock('../api', () => ({
  changeMemberRole: vi.fn(),
  removeMember: vi.fn(),
  resetUserPassword: vi.fn(),
}))
vi.mock('sonner', () => ({ toast: { success: vi.fn() } }))

const member: Member = {
  id: '50000000-0000-0000-0000-000000000005',
  workspace_id: '10000000-0000-0000-0000-000000000001',
  user_id: '60000000-0000-0000-0000-000000000006',
  role: 'member',
  user: { nickname: '李四', email: 'li@example.com' },
  created_at: '2026-07-30T00:00:00Z',
  updated_at: '2026-07-30T00:00:00Z',
}

async function renderActions(
  actorRole: 'owner' | 'admin' | 'member',
  isPlatformAdmin = false
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <MemberActions
        workspaceSlug='acme'
        actorRole={actorRole}
        isPlatformAdmin={isPlatformAdmin}
        member={member}
      />
    </QueryClientProvider>
  )
}

describe('member capability matrix', () => {
  it('keeps invitation navigation hidden for a platform admin with member role', () => {
    const items = buildWorkspaceNavigation('acme', 'member', true).flatMap(
      (group) => group.items
    )
    expect(items.map((item) => item.title)).not.toContain('邀请')
  })

  it('shows no member mutation actions to ordinary members and admins', async () => {
    for (const role of ['member', 'admin'] as const) {
      const screen = await renderActions(role)
      await expect
        .element(screen.getByRole('button', { name: '调整角色' }))
        .not.toBeInTheDocument()
      await expect
        .element(screen.getByRole('button', { name: '移除成员' }))
        .not.toBeInTheDocument()
    }
  })

  it('lets owners change roles and remove members', async () => {
    const screen = await renderActions('owner')
    await expect
      .element(screen.getByRole('button', { name: '调整角色' }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByRole('button', { name: '移除成员' }))
      .toBeInTheDocument()
  })

  it('shows password reset only to platform administrators', async () => {
    const screen = await renderActions('member', true)
    await expect
      .element(screen.getByRole('button', { name: '重置密码' }))
      .toBeInTheDocument()
  })

  it('preserves the last-owner constraint for 409 responses', () => {
    expect(
      memberActionErrorMessage(new ApiError('资源冲突', 409, 'conflict'))
    ).toBe('Workspace 必须至少保留一名 owner，请先将其他成员设为 owner')
  })
})
