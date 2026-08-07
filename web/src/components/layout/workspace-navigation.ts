import {
  BookOpen,
  Boxes,
  KeyRound,
  LayoutDashboard,
  Plug,
  SlidersHorizontal,
  Users,
} from 'lucide-react'
import type { Role } from '@/features/auth/types'
import i18n from '@/lib/i18n'
import type { NavGroup } from './types'

export function buildWorkspaceNavigation(
  workspaceSlug: string,
  role: Role,
  isPlatformAdmin: boolean
): NavGroup[] {
  const base = `/workspaces/${encodeURIComponent(workspaceSlug)}`
  const isAdminLike = role === 'owner' || role === 'admin'

  // 业务区：所有成员可见的高频功能。
  const workspaceItems: NavGroup['items'] = [
    {
      title: i18n.t('common.layout.navOverview'),
      url: base,
      exact: true,
      icon: LayoutDashboard,
    },
    {
      title: i18n.t('common.layout.navKnowledgeBases'),
      url: `${base}/kb`,
      activePaths: [`${base}/documents`, `${base}/jobs`],
      icon: BookOpen,
    },
    {
      title: i18n.t('common.layout.navModels'),
      url: `${base}/models`,
      icon: Boxes,
    },
  ]

  // 管理区：仅 owner/admin 可见的低频配置。
  // 顺序按"资源/人"在前、"访问/算法"在后：成员 → 集成 → API 密钥 → 检索策略。
  const adminItems: NavGroup['items'] = isAdminLike
    ? [
        {
          title: i18n.t('common.layout.navMembers'),
          url: `${base}/members`,
          icon: Users,
        },
        {
          title: i18n.t('common.layout.navIntegrations'),
          url: `${base}/integrations`,
          icon: Plug,
        },
        {
          title: i18n.t('common.layout.navApiKeys'),
          url: `${base}/api-keys`,
          icon: KeyRound,
        },
        {
          title: i18n.t('common.layout.navSearchSettings'),
          url: `${base}/search-settings`,
          icon: SlidersHorizontal,
        },
      ]
    : []

  const groups: NavGroup[] = [
    { title: i18n.t('common.layout.navWorkspace'), items: workspaceItems },
  ]
  if (isAdminLike) {
    groups.push({
      title: i18n.t('common.layout.navWorkspaceManagement'),
      items: adminItems,
    })
  }
  return [...groups, ...buildPlatformNavigation(isPlatformAdmin)]
}

export function buildPlatformNavigation(isPlatformAdmin: boolean): NavGroup[] {
  return isPlatformAdmin
    ? [
        {
          title: i18n.t('common.layout.navPlatformAdmin'),
          items: [
            {
              title: i18n.t('common.layout.navPlatformModels'),
              url: '/admin/models',
              icon: Boxes,
            },
          ],
        },
      ]
    : []
}
