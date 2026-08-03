import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import { SignOutDialog } from './sign-out-dialog'

const navigate = vi.hoisted(() => vi.fn())
const logout = vi.hoisted(() => vi.fn())

const MOCK_HREF = '/workspaces/acme/kb?tab=1'

vi.mock('@/features/auth/api', () => ({ logout }))

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    useNavigate: () => navigate,
    useLocation: () => ({ href: MOCK_HREF }),
  }
})

describe('SignOutDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('logs out and navigates to sign-in with the current safe location', async () => {
    logout.mockResolvedValue(undefined)
    const client = new QueryClient({
      defaultOptions: { mutations: { retry: false } },
    })
    const { getByRole } = await render(
      <QueryClientProvider client={client}>
        <SignOutDialog open onOpenChange={vi.fn()} />
      </QueryClientProvider>
    )

    await userEvent.click(getByRole('button', { name: '退出登录' }))

    await vi.waitFor(() => expect(logout).toHaveBeenCalledOnce())
    await vi.waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({
        to: '/sign-in',
        search: { redirect: MOCK_HREF },
        replace: true,
      })
    )
  })

  it('does not log out or navigate when cancellation is clicked', async () => {
    const client = new QueryClient()
    const { getByRole } = await render(
      <QueryClientProvider client={client}>
        <SignOutDialog open onOpenChange={vi.fn()} />
      </QueryClientProvider>
    )

    await userEvent.click(getByRole('button', { name: '取消' }))

    expect(logout).not.toHaveBeenCalled()
    expect(navigate).not.toHaveBeenCalled()
  })
})
