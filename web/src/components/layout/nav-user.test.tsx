import { beforeEach, describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-react'
import i18n from '@/lib/i18n'
import { NavUser } from './nav-user'

// Link 渲染为带 to 属性的锚点，便于断言目标路径
vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    Link: ({
      to,
      children,
      ...rest
    }: React.PropsWithChildren<{ to: string }>) => (
      <a href={to} data-to={to} {...rest}>
        {children}
      </a>
    ),
  }
})

vi.mock('@/components/sign-out-dialog', () => ({
  SignOutDialog: () => <div data-testid='sign-out-dialog' />,
}))

vi.mock('@tanstack/react-query', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-query')>()
  return {
    ...actual,
    useQuery: () => ({
      data: {
        user: { nickname: '张三', email: 'zhangsan@example.com' },
      },
    }),
  }
})

// 用可访问的原生元素替代 sidebar primitive，便于点击与无障碍断言
vi.mock('@/components/ui/sidebar', async (importOriginal) => {
  const actual =
    await importOriginal<typeof import('@/components/ui/sidebar')>()
  return {
    ...actual,
    SidebarMenu: ({ children }: React.PropsWithChildren) => (
      <div>{children}</div>
    ),
    SidebarMenuItem: ({ children }: React.PropsWithChildren) => (
      <div>{children}</div>
    ),
    SidebarMenuButton: ({
      children,
      ...rest
    }: React.PropsWithChildren<Record<string, unknown>>) => (
      <button
        type='button'
        {...(rest as React.ButtonHTMLAttributes<HTMLButtonElement>)}
      >
        {children}
      </button>
    ),
    useSidebar: () => ({ isMobile: false }),
  }
})

describe('NavUser', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('zh')
  })

  it('exposes account, appearance, and language settings entries', async () => {
    const screen = await render(<NavUser />)

    await userEvent.click(screen.getByRole('button'))

    const accountItem = screen.getByRole('menuitem', { name: '账号设置' })
    const appearanceItem = screen.getByRole('menuitem', { name: '外观设置' })
    const languageItem = screen.getByRole('menuitem', { name: '语言设置' })

    await expect
      .element(accountItem)
      .toHaveAttribute('data-to', '/settings/account')
    await expect
      .element(appearanceItem)
      .toHaveAttribute('data-to', '/settings/appearance')
    await expect
      .element(languageItem)
      .toHaveAttribute('data-to', '/settings/language')
  })
})
