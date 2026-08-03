import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import { canRevokeInvitation } from '../invitation-list'
import type { InvitationListItem } from '../types'
import { InvitationForm, invitableRoles } from './invitation-form'

const createInvitation = vi.hoisted(() => vi.fn())
vi.mock('../api', () => ({
  createInvitation,
  listInvitations: vi.fn(),
  revokeInvitation: vi.fn(),
  revokeInvitationAsPlatformAdmin: vi.fn(),
}))
vi.mock('sonner', () => ({ toast: { success: vi.fn() } }))

const invitation: InvitationListItem = {
  id: '70000000-0000-0000-0000-000000000007',
  workspace_id: '10000000-0000-0000-0000-000000000001',
  invited_email: 'new@example.com',
  role: 'member',
  token_prefix: 'abcd1234',
  status: 'pending',
  expires_at: '2026-08-06T00:00:00Z',
  accepted_at: null,
  revoked_at: null,
  created_by: '80000000-0000-0000-0000-000000000008',
  created_at: '2026-07-30T00:00:00Z',
}

async function renderForm(actorRole: 'owner' | 'admin') {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const invalidateQueries = vi.spyOn(client, 'invalidateQueries')
  const screen = await render(
    <QueryClientProvider client={client}>
      <InvitationForm workspaceSlug='acme' actorRole={actorRole} />
    </QueryClientProvider>
  )
  return { screen, invalidateQueries }
}

describe('invitation capability matrix', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    createInvitation.mockResolvedValue({
      id: invitation.id,
      invited_email: invitation.invited_email,
      role: invitation.role,
      expires_at: invitation.expires_at,
      token_prefix: invitation.token_prefix,
      invite_url: 'https://langhuan.example/invitations/one-time-token',
    })
  })

  it('allows admins to invite member/admin but only owners to invite owner', () => {
    expect(invitableRoles('admin')).toEqual(['member', 'admin'])
    expect(invitableRoles('owner')).toEqual(['member', 'admin', 'owner'])
  })

  it('lets admin revoke only their own pending invitations', () => {
    expect(
      canRevokeInvitation(invitation, {
        actorRole: 'admin',
        actorUserId: invitation.created_by,
        isPlatformAdmin: false,
      })
    ).toBe(true)
    expect(
      canRevokeInvitation(invitation, {
        actorRole: 'admin',
        actorUserId: 'someone-else',
        isPlatformAdmin: false,
      })
    ).toBe(false)
    expect(
      canRevokeInvitation(
        { ...invitation, status: 'accepted' },
        {
          actorRole: 'owner',
          actorUserId: 'owner-id',
          isPlatformAdmin: false,
        }
      )
    ).toBe(false)
  })

  it('shows the one-time invite URL and refreshes the invitation list', async () => {
    const { screen, invalidateQueries } = await renderForm('admin')
    await userEvent.fill(screen.getByLabelText('受邀邮箱'), 'new@example.com')
    await userEvent.click(screen.getByRole('button', { name: '发出邀请' }))

    await vi.waitFor(() => expect(createInvitation).toHaveBeenCalledOnce())
    expect(createInvitation).toHaveBeenCalledWith('acme', {
      invited_email: 'new@example.com',
      role: 'member',
    })
    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: ['invitations', 'acme'],
    })
    await expect
      .element(
        screen.getByText('https://langhuan.example/invitations/one-time-token')
      )
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('关闭后无法再次查看完整链接'))
      .toBeInTheDocument()
  })
})
