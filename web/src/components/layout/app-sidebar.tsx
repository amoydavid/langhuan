import { useQuery } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarRail,
} from '@/components/ui/sidebar'
import { LAST_WORKSPACE_SLUG_KEY } from '@/features/auth/navigation'
import { meQueryOptions } from '@/features/auth/queries'
import { NavGroup } from './nav-group'
import { NavUser } from './nav-user'
import {
  buildPlatformNavigation,
  buildWorkspaceNavigation,
} from './workspace-navigation'
import { WorkspaceSwitcher } from './workspace-switcher'

export function AppSidebar() {
  // 订阅语言变化：buildWorkspaceNavigation 内部使用全局 i18n.t()（惰性求值但
  // 不触发 React 重渲染），此处通过 useTranslation 让侧边栏在切换语言后重渲染。
  useTranslation()
  const { data: me } = useQuery(meQueryOptions())
  const params = useParams({ strict: false }) as { workspaceSlug?: string }
  const recentSlug = localStorage.getItem(LAST_WORKSPACE_SLUG_KEY) ?? undefined
  // 优先使用 URL 中的 workspace（与当前页面上下文一致）；在 /admin、/settings
  // 等非 workspace 路由下回退到最近使用/第一个 workspace，保证「工作区」分组稳定。
  const membership =
    me?.workspaces.find(
      (workspace) => workspace.slug === params.workspaceSlug
    ) ??
    me?.workspaces.find((workspace) => workspace.slug === recentSlug) ??
    me?.workspaces[0]
  const isPlatformAdmin = me?.user.is_platform_admin ?? false
  const navGroups = membership
    ? buildWorkspaceNavigation(
        membership.slug,
        membership.role,
        isPlatformAdmin
      )
    : buildPlatformNavigation(isPlatformAdmin)

  return (
    <Sidebar collapsible='icon' variant='sidebar'>
      <SidebarHeader className='border-sidebar-border border-b p-2.5'>
        <WorkspaceSwitcher />
      </SidebarHeader>
      <SidebarContent className='px-1.5 py-2'>
        {navGroups.map((props) => (
          <NavGroup key={props.title} {...props} />
        ))}
      </SidebarContent>
      <SidebarFooter className='border-sidebar-border border-t p-2'>
        <NavUser />
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  )
}
