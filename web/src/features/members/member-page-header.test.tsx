import { describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import { MemberPageHeader } from './member-page-header'

vi.mock('@tanstack/react-router', () => ({
  Link: ({
    children,
    params,
    to,
  }: {
    children: React.ReactNode
    params: { workspaceSlug: string }
    to: string
  }) => (
    <a href={to.replace('$workspaceSlug', params.workspaceSlug)}>{children}</a>
  ),
}))

describe('MemberPageHeader', () => {
  it('links administrators to invitation management', async () => {
    const screen = await render(
      <MemberPageHeader workspaceSlug='acme' actorRole='admin' />
    )

    const link = screen.getByRole('link', { name: '管理邀请' })
    await expect.element(link).toBeVisible()
    await expect
      .element(link)
      .toHaveAttribute('href', '/workspaces/acme/invitations')
  })

  it('hides invitation management from ordinary members', async () => {
    const screen = await render(
      <MemberPageHeader workspaceSlug='acme' actorRole='member' />
    )

    await expect
      .element(screen.getByRole('link', { name: '管理邀请' }))
      .not.toBeInTheDocument()
  })
})
