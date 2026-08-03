import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import { SidebarProvider } from '@/components/ui/sidebar'
import type { MeResponse } from '@/features/auth/types'
import { WorkspaceSwitcher } from './workspace-switcher'

const navigate = vi.hoisted(() => vi.fn())
vi.mock('@/hooks/use-mobile', () => ({ useIsMobile: () => false }))
vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    useNavigate: () => navigate,
    useParams: () => ({ workspaceSlug: 'acme' }),
  }
})

const me: MeResponse = {
  user: {
    id: 'user-id',
    email: 'admin@example.com',
    nickname: '管理员',
    is_platform_admin: true,
  },
  workspaces: [
    { workspace_id: 'ws-1', slug: 'acme', name: 'Acme', role: 'owner' },
    { workspace_id: 'ws-2', slug: 'beta', name: 'Beta', role: 'member' },
  ],
}

async function renderSwitcher() {
  const client = new QueryClient()
  client.setQueryData(['me'], me)
  return render(
    <QueryClientProvider client={client}>
      <SidebarProvider>
        <WorkspaceSwitcher />
      </SidebarProvider>
    </QueryClientProvider>
  )
}

describe('WorkspaceSwitcher', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
  })

  it('shows the line brand mark without changing the workspace trigger', async () => {
    const screen = await renderSwitcher()
    const trigger = screen.getByRole('button', { name: /Acme/ }).element()

    expect(trigger.querySelector('[data-logo-variant="line"]')).not.toBeNull()
  })

  it('uses the URL workspace and navigates when another workspace is chosen', async () => {
    const screen = await renderSwitcher()
    await userEvent.click(screen.getByRole('button', { name: /Acme/ }))
    await userEvent.click(screen.getByRole('menuitem', { name: /Beta/ }))

    expect(localStorage.getItem('langhuan:last-workspace-slug')).toBe('beta')
    expect(navigate).toHaveBeenCalledWith({
      to: '/workspaces/beta/kb',
    })
  })

  it('shows workspace creation only to platform administrators', async () => {
    const screen = await renderSwitcher()
    await userEvent.click(screen.getByRole('button', { name: /Acme/ }))
    await expect
      .element(screen.getByRole('menuitem', { name: '创建工作区' }))
      .toBeInTheDocument()
  })
})
