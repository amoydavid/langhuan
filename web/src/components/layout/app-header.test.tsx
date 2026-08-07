import { describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import { AppHeader } from './app-header'

vi.mock('@/components/ui/sidebar', () => ({
  SidebarTrigger: () => <button type='button'>侧边栏</button>,
}))
vi.mock('@/components/ui/separator', () => ({
  Separator: () => <span aria-hidden='true' />,
}))
vi.mock('@/components/search', () => ({
  Search: () => <button type='button'>搜索</button>,
}))
vi.mock('@/components/theme-switch', () => ({
  ThemeSwitch: () => <button type='button'>主题</button>,
}))
vi.mock('@/components/profile-dropdown', () => ({
  ProfileDropdown: () => <button type='button'>用户</button>,
}))
vi.mock('@/components/layout/app-breadcrumbs', () => ({
  AppBreadcrumbs: () => <nav>面包屑</nav>,
}))

describe('AppHeader', () => {
  it('keeps the fixed header controls in the approved DOM order', async () => {
    await render(<AppHeader fixed />)

    const header = document.querySelector('[data-testid="app-header"]')
    expect(header).not.toBeNull()
    expect(header?.className).toContain('sticky')
    expect(header?.className).toContain('h-14')
    expect(header?.className).toContain('shrink-0')
    expect(
      Array.from(header?.querySelectorAll('[data-header-item]') ?? []).map(
        (element) => element.getAttribute('data-header-item')
      )
    ).toEqual([
      'trigger',
      'breadcrumbs',
      'spacer',
      'search',
      'theme',
      'language',
      'profile',
    ])
  })
})
