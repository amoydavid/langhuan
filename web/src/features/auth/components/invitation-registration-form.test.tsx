import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import { InvitationRegistrationForm } from './invitation-registration-form'

const navigate = vi.hoisted(() => vi.fn())
const registerUser = vi.hoisted(() => vi.fn())
const getMe = vi.hoisted(() => vi.fn())

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return { ...actual, useNavigate: () => navigate }
})
vi.mock('@/features/auth/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/auth/api')>()),
  registerUser,
  getMe,
}))

const invitation = {
  workspace_id: '20000000-0000-0000-0000-000000000002',
  workspace_name: 'Acme',
  workspace_slug: 'acme',
  invited_email: 'invitee@example.com',
  role: 'member' as const,
  expires_at: '2026-08-01T00:00:00Z',
}

async function renderForm() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <InvitationRegistrationForm
        invitation={invitation}
        token='invite-token'
      />
    </QueryClientProvider>
  )
}

describe('InvitationRegistrationForm', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    registerUser.mockResolvedValue({ id: 'user-id' })
    getMe.mockResolvedValue({ user: {}, workspaces: [] })
  })

  it('locks the invited email', async () => {
    const screen = await renderForm()
    await expect
      .element(screen.getByLabelText('邮箱'))
      .toHaveValue('invitee@example.com')
    await expect.element(screen.getByLabelText('邮箱')).toBeDisabled()
  })

  it('accepts the invitation, refreshes me, and enters its workspace', async () => {
    const screen = await renderForm()
    await userEvent.fill(screen.getByLabelText('昵称'), '受邀用户')
    await userEvent.fill(
      screen.getByLabelText('密码', { exact: true }),
      'password'
    )
    await userEvent.fill(screen.getByLabelText('确认密码'), 'password')
    await userEvent.click(screen.getByRole('button', { name: '接受邀请' }))

    await vi.waitFor(() => expect(registerUser).toHaveBeenCalledOnce())
    expect(registerUser).toHaveBeenCalledWith({
      email: 'invitee@example.com',
      nickname: '受邀用户',
      password: 'password',
      invitation_token: 'invite-token',
    })
    await vi.waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({
        to: '/workspaces/acme/kb',
        replace: true,
      })
    )
  })
})
