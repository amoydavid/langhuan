import { useEffect } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import { SidebarProvider, useSidebar } from '@/components/ui/sidebar'
import { isNavActive, NavGroup } from './nav-group'

vi.mock('@/hooks/use-mobile', () => ({ useIsMobile: () => true }))
vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    useLocation: () => '/workspaces/acme/kb/123',
    Link: ({
      to,
      children,
      onClick,
    }: {
      to: string
      children: React.ReactNode
      onClick?: React.MouseEventHandler<HTMLAnchorElement>
    }) => (
      <a
        href={to}
        onClick={(event) => {
          event.preventDefault()
          onClick?.(event)
        }}
      >
        {children}
      </a>
    ),
  }
})

describe('isNavActive', () => {
  it('activates nested knowledge base and member paths', () => {
    expect(
      isNavActive('/workspaces/acme/kb/123', {
        title: '知识库',
        url: '/workspaces/acme/kb',
      })
    ).toBe(true)
    expect(
      isNavActive('/workspaces/acme/members?tab=pending', {
        title: '成员',
        url: '/workspaces/acme/members',
      })
    ).toBe(true)
  })

  it('activates only the most specific sibling for nested workspace routes', () => {
    const items = [
      { title: '概览', url: '/workspaces/demo' },
      { title: '知识库', url: '/workspaces/demo/kb' },
      { title: '成员', url: '/workspaces/demo/members' },
    ]

    expect(
      items
        .filter((item) =>
          isNavActive('/workspaces/demo/kb/123?tab=documents', item, items)
        )
        .map((item) => item.title)
    ).toEqual(['知识库'])
  })

  it('supports exact overview matching and explicit related route families', () => {
    const items = [
      { title: '概览', url: '/workspaces/demo', exact: true },
      {
        title: '知识库',
        url: '/workspaces/demo/kb',
        activePaths: ['/workspaces/demo/documents', '/workspaces/demo/jobs'],
      },
      { title: '成员', url: '/workspaces/demo/members' },
    ]

    for (const [href, expected] of [
      ['/workspaces/demo', '概览'],
      ['/workspaces/demo/kb/new', '知识库'],
      ['/workspaces/demo/documents/123', '知识库'],
      ['/workspaces/demo/jobs/456?from=document', '知识库'],
      ['/workspaces/demo/members', '成员'],
    ] as const) {
      expect(
        items
          .filter((item) => isNavActive(href, item, items))
          .map((item) => item.title),
        href
      ).toEqual([expected])
    }
  })
})

function MobileNavHarness() {
  const { openMobile, setOpenMobile } = useSidebar()
  useEffect(() => setOpenMobile(true), [setOpenMobile])
  return (
    <>
      <output aria-label='移动导航状态'>{openMobile ? '打开' : '关闭'}</output>
      <NavGroup
        title='Workspace'
        items={[
          {
            title: '知识库',
            url: '/workspaces/acme/kb',
          },
        ]}
      />
    </>
  )
}

describe('NavGroup mobile behavior', () => {
  it('closes the mobile Sheet after navigation', async () => {
    const screen = await render(
      <SidebarProvider>
        <MobileNavHarness />
      </SidebarProvider>
    )
    await expect
      .element(screen.getByLabelText('移动导航状态'))
      .toHaveTextContent('打开')
    await userEvent.click(screen.getByRole('link', { name: '知识库' }))
    await expect
      .element(screen.getByLabelText('移动导航状态'))
      .toHaveTextContent('关闭')
  })
})
