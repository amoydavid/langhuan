import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import { CompleteProfile } from './complete-profile'

const mocks = vi.hoisted(() => ({
  updateProfile: vi.fn(),
  hardRedirect: vi.fn(),
  search: { next: '/dashboard' } as {
    next?: string
    invitation_token_hash?: string
  },
}))

vi.mock('@/features/auth/api', () => ({
  updateProfile: mocks.updateProfile,
}))

vi.mock('@/routes/(auth)/complete-profile', () => ({
  Route: { useSearch: () => mocks.search },
}))

vi.mock('./navigation', () => ({
  hardRedirect: mocks.hardRedirect,
}))

async function renderCompleteProfile() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <CompleteProfile />
    </QueryClientProvider>
  )
}

describe('CompleteProfile', () => {
  it('shows the skip action for a regular OIDC login without email', async () => {
    mocks.search = { next: '/dashboard' }
    const screen = await renderCompleteProfile()
    await expect
      .element(screen.getByRole('button', { name: '跳过，稍后再说' }))
      .toBeInTheDocument()
  })

  it('skips to the original destination without saving an email', async () => {
    mocks.search = { next: '/dashboard' }
    const screen = await renderCompleteProfile()
    await userEvent.click(
      screen.getByRole('button', { name: '跳过，稍后再说' })
    )
    expect(mocks.hardRedirect).toHaveBeenCalledWith('/dashboard')
    expect(mocks.updateProfile).not.toHaveBeenCalled()
  })

  it('hides the skip action when completing an invitation (email required)', async () => {
    mocks.search = { next: '/dashboard', invitation_token_hash: 'hash-1' }
    const screen = await renderCompleteProfile()
    await expect
      .element(screen.getByRole('button', { name: '跳过，稍后再说' }))
      .not.toBeInTheDocument()
  })
})
