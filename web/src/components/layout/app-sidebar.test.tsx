import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import { LAST_WORKSPACE_SLUG_KEY } from '@/features/auth/navigation'
import i18n from '@/lib/i18n'
import { AppSidebar } from './app-sidebar'

vi.mock('@tanstack/react-query', () => ({
  useQuery: () => ({
    data: {
      user: { is_platform_admin: false },
      workspaces: [
        { slug: 'acme', role: 'owner' },
        { slug: 'beta', role: 'member' },
      ],
    },
  }),
}))
const paramsMock = vi.hoisted(() => ({
  workspaceSlug: undefined as string | undefined,
}))
vi.mock('@tanstack/react-router', () => ({
  useParams: () => paramsMock,
}))
vi.mock('@/features/auth/queries', () => ({
  meQueryOptions: () => ({}),
}))
vi.mock('@/components/ui/sidebar', () => ({
  Sidebar: ({
    children,
    collapsible,
    variant,
  }: React.PropsWithChildren<{
    collapsible?: string
    variant?: string
  }>) => (
    <aside
      data-testid='app-sidebar-shell'
      data-collapsible={collapsible}
      data-variant={variant}
    >
      {children}
    </aside>
  ),
  SidebarContent: ({ children }: React.PropsWithChildren) => (
    <div>{children}</div>
  ),
  SidebarFooter: ({ children }: React.PropsWithChildren) => (
    <footer>{children}</footer>
  ),
  SidebarHeader: ({ children }: React.PropsWithChildren) => (
    <header>{children}</header>
  ),
  SidebarRail: () => null,
}))
vi.mock('./workspace-switcher', () => ({
  WorkspaceSwitcher: () => <div>Workspace switcher</div>,
}))
vi.mock('./nav-user', () => ({ NavUser: () => <div>User</div> }))
// 渲染导航菜单项标题以便断言（标题由 workspace-navigation 生成）
vi.mock('./nav-group', () => ({
  NavGroup: ({
    title,
    items,
  }: {
    title: string
    items: { title: string }[]
  }) => (
    <nav>
      <span>{title}</span>
      {items.map((item) => (
        <span key={item.title}>{item.title}</span>
      ))}
    </nav>
  ),
}))

describe('AppSidebar', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('zh')
    localStorage.clear()
    paramsMock.workspaceSlug = 'acme'
  })

  it('keeps the existing header interaction in a fixed sidebar shell', async () => {
    await render(<AppSidebar />)

    const shell = document.querySelector('[data-testid="app-sidebar-shell"]')
    expect(shell?.getAttribute('data-variant')).toBe('sidebar')
    expect(shell?.getAttribute('data-collapsible')).toBe('icon')
    expect(shell?.querySelector('header')?.textContent).toContain(
      'Workspace switcher'
    )
  })

  it('re-renders navigation titles after the language switches', async () => {
    const { container } = await render(<AppSidebar />)
    expect(container.textContent).toContain('概览')
    expect(container.textContent).toContain('知识库')

    await i18n.changeLanguage('en')

    // AppSidebar 订阅语言变化后重渲染，导航标题应切换为英文
    expect(container.textContent).toContain('Overview')
    expect(container.textContent).toContain('Knowledge bases')
    expect(container.textContent).toContain('Models')
  })

  it('keeps the workspace group on non-workspace routes using the last used workspace', async () => {
    localStorage.setItem(LAST_WORKSPACE_SLUG_KEY, 'beta')
    paramsMock.workspaceSlug = undefined
    const { container } = await render(<AppSidebar />)

    expect(container.textContent).toContain('工作区')
    expect(container.textContent).toContain('知识库')
    // beta 是 member，API Key 入口不应显示
    expect(container.textContent).not.toContain('API Key')
  })

  it('falls back to the first workspace when no slug is known', async () => {
    paramsMock.workspaceSlug = undefined
    const { container } = await render(<AppSidebar />)

    expect(container.textContent).toContain('工作区')
    expect(container.textContent).toContain('概览')
    expect(container.textContent).toContain('API Key')
  })
})
