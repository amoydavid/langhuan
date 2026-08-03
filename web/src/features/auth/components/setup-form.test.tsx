import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import { SetupForm } from './setup-form'

const navigate = vi.hoisted(() => vi.fn())
const registerUser = vi.hoisted(() => vi.fn())
const getBootstrapStatus = vi.hoisted(() => vi.fn())

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return { ...actual, useNavigate: () => navigate }
})
vi.mock('@/features/auth/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/auth/api')>()),
  registerUser,
  getBootstrapStatus,
}))
vi.mock('sonner', () => ({ toast: { success: vi.fn() } }))

async function renderForm() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <SetupForm />
    </QueryClientProvider>
  )
}

describe('SetupForm', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    registerUser.mockResolvedValue({ id: 'user-id' })
    getBootstrapStatus.mockResolvedValue({ initialized: true })
  })

  it('rejects mismatched passwords', async () => {
    const screen = await renderForm()
    await userEvent.fill(screen.getByLabelText('邮箱'), 'admin@example.com')
    await userEvent.fill(screen.getByLabelText('昵称'), '管理员')
    await userEvent.fill(
      screen.getByLabelText('密码', { exact: true }),
      'password'
    )
    await userEvent.fill(screen.getByLabelText('确认密码'), 'different')
    await userEvent.click(screen.getByRole('button', { name: '完成初始化' }))

    await expect
      .element(screen.getByText('两次输入的密码不一致'))
      .toBeInTheDocument()
    expect(registerUser).not.toHaveBeenCalled()
  })

  it('registers the first administrator without a confirm field', async () => {
    const screen = await renderForm()
    await userEvent.fill(screen.getByLabelText('邮箱'), 'admin@example.com')
    await userEvent.fill(screen.getByLabelText('昵称'), '管理员')
    await userEvent.fill(
      screen.getByLabelText('密码', { exact: true }),
      'password'
    )
    await userEvent.fill(screen.getByLabelText('确认密码'), 'password')
    await userEvent.click(screen.getByRole('button', { name: '完成初始化' }))

    await vi.waitFor(() => expect(registerUser).toHaveBeenCalledOnce())
    expect(registerUser).toHaveBeenCalledWith({
      email: 'admin@example.com',
      nickname: '管理员',
      password: 'password',
    })
    expect(getBootstrapStatus).toHaveBeenCalledOnce()
    expect(navigate).toHaveBeenCalledWith({ to: '/sign-in', replace: true })
  })

  it('moves focus from password to confirmation without stopping at visibility toggle', async () => {
    const screen = await renderForm()
    const password = screen.getByLabelText('密码', { exact: true })
    const confirmation = screen.getByLabelText('确认密码')

    await userEvent.click(password)
    await userEvent.tab()

    await expect.element(confirmation).toHaveFocus()
  })
})
