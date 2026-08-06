import {
  BookOpen,
  Boxes,
  KeyRound,
  LayoutDashboard,
  MailPlus,
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
  const items: NavGroup['items'] = [
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
    {
      title: i18n.t('common.layout.navMembers'),
      url: `${base}/members`,
      icon: Users,
    },
  ]
  if (role === 'owner' || role === 'admin') {
    items.push({
      title: i18n.t('common.layout.navInvitations'),
      url: `${base}/invitations`,
      icon: MailPlus,
    })
    items.push({ title: 'API Key', url: `${base}/api-keys`, icon: KeyRound })
    items.push({
      title: i18n.t('common.layout.navSearchSettings'),
      url: `${base}/search-settings`,
      icon: SlidersHorizontal,
    })
  }
  return [
    { title: i18n.t('common.layout.navWorkspace'), items },
    ...buildPlatformNavigation(isPlatformAdmin),
  ]
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
