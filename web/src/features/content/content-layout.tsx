import {
  Link,
  Outlet,
  useLocation,
  useMatches,
  useNavigate,
} from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type { KnowledgeBaseSummary } from '@/features/knowledge-bases/workbench/types'
import i18n from '@/lib/i18n'
import { cn } from '@/lib/utils'

type ContentTab = {
  label: string
  countLabel: string
  href: string
  segment: string
}

function contentTabs(
  workspaceSlug: string,
  kbId: string,
  summary: KnowledgeBaseSummary,
  webSourceCount: number | undefined
): ContentTab[] {
  const base = `/workspaces/${encodeURIComponent(workspaceSlug)}/kb/${encodeURIComponent(kbId)}/content`
  const counts = summary.document_counts
  // Web tab 恒显示（v1.3.0 spec §9.4）：它承载网站内容源的管理入口——若按
  // 「存在 web 源或 web 文档」条件隐藏，全新知识库将没有任何 UI 入口能创建
  // 第一个网站源（鸡生蛋死锁，2026-09-18 用户实测暴露，E2E 此前靠深链绕过）。
  // 空态自带角色门控：admin/owner 见「添加网站源」，member 见说明文案。
  // webSourceCount 为 undefined 表示源列表尚未加载，计数回退为 web 文档数。
  const webCountLabel =
    webSourceCount !== undefined && webSourceCount > 0
      ? i18n.t('content.contentLayout.webTabCount', { count: webSourceCount })
      : String(counts.web)
  return [
    {
      label: i18n.t('content.contentLayout.tabAll'),
      countLabel: String(counts.total),
      href: `${base}/all`,
      segment: 'all',
    },
    {
      label: i18n.t('content.contentLayout.tabFiles'),
      countLabel: String(counts.file),
      href: `${base}/files`,
      segment: 'files',
    },
    {
      label: i18n.t('content.contentLayout.tabFaq'),
      countLabel: String(counts.faq),
      href: `${base}/faq`,
      segment: 'faq',
    },
    {
      label: i18n.t('content.contentLayout.tabWeb'),
      countLabel: webCountLabel,
      href: `${base}/web`,
      segment: 'web',
    },
  ]
}

type ContentLayoutProps = {
  workspaceSlug: string
  kbId: string
  summary: KnowledgeBaseSummary
  /** 该 KB 的 web 内容源数量；undefined 表示尚未加载（按文档数回退）。 */
  webSourceCount?: number
  children?: ReactNode
}

export function ContentLayout({
  workspaceSlug,
  kbId,
  summary,
  webSourceCount,
  children,
}: ContentLayoutProps) {
  const { t } = useTranslation()
  const location = useLocation()
  const navigate = useNavigate()
  const matches = useMatches()
  const fullHeight = matches.some((m) => m.staticData.fullHeight === true)
  const tabs = contentTabs(workspaceSlug, kbId, summary, webSourceCount)
  const current =
    tabs.find(
      (tab) =>
        location.pathname === tab.href ||
        location.pathname.startsWith(`${tab.href}/`)
    ) ?? tabs[0]

  return (
    <section
      className={cn(
        'space-y-4',
        fullHeight && 'flex h-full flex-col overflow-hidden'
      )}
    >
      <nav
        aria-label={t('content.contentLayout.tabsAriaLabel')}
        className={cn(
          'hidden flex-wrap gap-1 md:flex',
          fullHeight && 'shrink-0'
        )}
      >
        {tabs.map((tab) => {
          const selected = tab.segment === current?.segment
          return (
            <Link
              key={tab.segment}
              to={tab.href}
              aria-current={selected ? 'page' : undefined}
              className={cn(
                'rounded-md px-3 py-2 font-medium text-sm transition-colors',
                selected
                  ? 'bg-primary/10 text-primary'
                  : 'text-muted-foreground hover:bg-muted hover:text-foreground'
              )}
            >
              {tab.label} {tab.countLabel}
            </Link>
          )
        })}
      </nav>
      <div className='md:hidden'>
        <Select
          value={current?.href}
          onValueChange={(href) => void navigate({ to: href })}
        >
          <SelectTrigger
            className='w-full'
            aria-label={t('content.contentLayout.tabsAriaLabel')}
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {tabs.map((tab) => (
              <SelectItem key={tab.segment} value={tab.href}>
                {tab.label} {tab.countLabel}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div
        className={cn('min-w-0', fullHeight && 'flex min-h-0 flex-1 flex-col')}
      >
        {children ?? <Outlet />}
      </div>
    </section>
  )
}
