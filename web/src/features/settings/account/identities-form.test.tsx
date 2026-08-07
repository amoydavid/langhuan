import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import type { ExternalIdentity } from '@/features/auth/types'
import { IdentitiesForm } from './identities-form'

const startOIDCBind = vi.hoisted(() => vi.fn())

vi.mock('@/features/auth/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/auth/api')>()),
  startOIDCBind,
}))

const boundIdentity: ExternalIdentity = {
  issuer: 'https://sso.example.com',
  email: 'ada@example.com',
  last_auth_at: '2026-08-07T00:00:00Z',
}

async function renderForm(identities: ExternalIdentity[]) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(['external-identities'], identities)
  return render(
    <QueryClientProvider client={client}>
      <IdentitiesForm oidcEnabled />
    </QueryClientProvider>
  )
}

describe('IdentitiesForm', () => {
  it('hides the bind button when an OIDC identity is already bound', async () => {
    const screen = await renderForm([boundIdentity])
    await expect
      .element(screen.getByRole('button', { name: '绑定企业 SSO' }))
      .not.toBeInTheDocument()
  })

  it('keeps the bind button when no identity is bound', async () => {
    const screen = await renderForm([])
    await expect
      .element(screen.getByRole('button', { name: '绑定企业 SSO' }))
      .toBeInTheDocument()
  })
})
