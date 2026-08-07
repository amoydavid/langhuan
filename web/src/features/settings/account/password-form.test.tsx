import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import { PasswordForm } from './password-form'

const changePassword = vi.hoisted(() => vi.fn())

vi.mock('@/features/auth/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/auth/api')>()),
  changePassword,
}))

async function renderForm() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <PasswordForm />
    </QueryClientProvider>
  )
}

describe('PasswordForm', () => {
  it('submits old and new password', async () => {
    changePassword.mockResolvedValue(undefined)
    const screen = await renderForm()

    await userEvent.fill(
      screen.getByPlaceholder('请输入当前密码'),
      'old-secret'
    )
    await userEvent.fill(screen.getByPlaceholder('至少 8 位'), 'new-secret-1')
    await userEvent.fill(
      screen.getByPlaceholder('再次输入新密码'),
      'new-secret-1'
    )

    await userEvent.click(screen.getByRole('button', { name: '更新密码' }))

    expect(changePassword).toHaveBeenCalledWith({
      old_password: 'old-secret',
      new_password: 'new-secret-1',
    })
  })

  it('blocks submission when passwords do not match', async () => {
    changePassword.mockClear()
    const screen = await renderForm()

    await userEvent.fill(
      screen.getByPlaceholder('请输入当前密码'),
      'old-secret'
    )
    await userEvent.fill(screen.getByPlaceholder('至少 8 位'), 'new-secret-1')
    await userEvent.fill(
      screen.getByPlaceholder('再次输入新密码'),
      'different-1'
    )

    await userEvent.click(screen.getByRole('button', { name: '更新密码' }))

    expect(changePassword).not.toHaveBeenCalled()
  })
})
