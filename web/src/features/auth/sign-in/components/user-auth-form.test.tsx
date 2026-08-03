import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import { UserAuthForm } from './user-auth-form'

const navigate = vi.hoisted(() => vi.fn())
const login = vi.hoisted(() => vi.fn())
const getMe = vi.hoisted(() => vi.fn())

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return { ...actual, useNavigate: () => navigate }
})

vi.mock('@/features/auth/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/auth/api')>()),
  login,
  getMe,
}))

async function renderForm(redirectTo?: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <UserAuthForm redirectTo={redirectTo} />
    </QueryClientProvider>
  )
}

describe('UserAuthForm', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    login.mockResolvedValue({
      user_id: '10000000-0000-0000-0000-000000000001',
    })
    getMe.mockResolvedValue({
      user: {
        id: '10000000-0000-0000-0000-000000000001',
        email: 'user@example.com',
        nickname: '用户',
        is_platform_admin: false,
      },
      workspaces: [
        {
          workspace_id: '20000000-0000-0000-0000-000000000002',
          slug: 'acme',
          name: 'Acme',
          role: 'member',
        },
      ],
    })
  })

  it('renders only the real email/password login flow', async () => {
    const screen = await renderForm()

    await expect.element(screen.getByLabelText('邮箱')).toBeInTheDocument()
    await expect.element(screen.getByLabelText('密码')).toBeInTheDocument()
    await expect
      .element(screen.getByRole('button', { name: '登录' }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText(/忘记密码|GitHub|Facebook/))
      .not.toBeInTheDocument()
  })

  it('logs in, refreshes me, and enters the only workspace', async () => {
    const screen = await renderForm()
    await userEvent.fill(screen.getByLabelText('邮箱'), 'user@example.com')
    await userEvent.fill(screen.getByLabelText('密码'), 'password')
    await userEvent.click(screen.getByRole('button', { name: '登录' }))

    await vi.waitFor(() => expect(login).toHaveBeenCalledOnce())
    expect(login).toHaveBeenCalledWith({
      email: 'user@example.com',
      password: 'password',
    })
    await vi.waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({
        to: '/workspaces/acme/kb',
        replace: true,
      })
    )
  })

  it('rejects an external redirect after login', async () => {
    const screen = await renderForm('https://evil.example')
    await userEvent.fill(screen.getByLabelText('邮箱'), 'user@example.com')
    await userEvent.fill(screen.getByLabelText('密码'), 'password')
    await userEvent.click(screen.getByRole('button', { name: '登录' }))

    await vi.waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({
        to: '/workspaces/acme/kb',
        replace: true,
      })
    )
  })
})
