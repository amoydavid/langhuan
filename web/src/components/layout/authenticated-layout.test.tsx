import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'

vi.mock('@tanstack/react-router', () => ({
  Outlet: () => null,
  useMatches: () => [],
}))
vi.mock('@/components/layout/app-header', () => ({
  AppHeader: () => <header data-testid='app-header' />,
}))
vi.mock('@/components/layout/app-sidebar', () => ({
  AppSidebar: () => null,
}))
vi.mock('@/components/skip-to-main', () => ({
  SkipToMain: () => null,
}))
vi.mock('@/context/search-provider', () => ({
  SearchProvider: ({ children }: { children: ReactNode }) => children,
}))
vi.mock('@/lib/cookies', () => ({
  getCookie: () => undefined,
}))
vi.mock('@/components/ui/sidebar', () => ({
  SidebarInset: ({
    className,
    children,
  }: {
    className?: string
    children: ReactNode
  }) => (
    <div data-testid='sidebar-inset' className={className}>
      {children}
    </div>
  ),
  SidebarProvider: ({ children }: { children: ReactNode }) => children,
}))

import { AuthenticatedLayout } from './authenticated-layout'

describe('AuthenticatedLayout', () => {
  it('owns page scrolling so modal scroll locks cannot hide the sticky header', async () => {
    await render(<AuthenticatedLayout />)

    const inset = document.querySelector('[data-testid="sidebar-inset"]')
    expect(inset?.className).toContain('h-svh')
    expect(inset?.className).toContain('overflow-auto')
  })
})
