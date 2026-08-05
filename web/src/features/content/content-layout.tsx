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

type ContentLayoutProps = {
  workspaceSlug: string
  kbId: string
  summary: KnowledgeBaseSummary
  children?: ReactNode
}

type ContentTab = {
  label: string
  count: number
  href: string
  segment: string
}

function contentTabs(
  workspaceSlug: string,
  kbId: string,
  summary: KnowledgeBaseSummary
): ContentTab[] {
  const base = `/workspaces/${encodeURIComponent(workspaceSlug)}/kb/${encodeURIComponent(kbId)}/content`
  const counts = summary.document_counts
  return [
    {
      label: i18n.t('content.contentLayout.tabAll'),
      count: counts.total,
      href: `${base}/all`,
      segment: 'all',
    },
    {
      label: i18n.t('content.contentLayout.tabFiles'),
      count: counts.file,
      href: `${base}/files`,
      segment: 'files',
    },
    {
      label: i18n.t('content.contentLayout.tabFaq'),
      count: counts.faq,
      href: `${base}/faq`,
      segment: 'faq',
    },
    ...(counts.web > 0
      ? [
          {
            label: i18n.t('content.contentLayout.tabWeb'),
            count: counts.web,
            href: `${base}/web`,
            segment: 'web',
          },
        ]
      : []),
  ]
}

export function ContentLayout({
  workspaceSlug,
  kbId,
  summary,
  children,
}: ContentLayoutProps) {
  const { t } = useTranslation()
  const location = useLocation()
  const navigate = useNavigate()
  const matches = useMatches()
  const fullHeight = matches.some((m) => m.staticData.fullHeight === true)
  const tabs = contentTabs(workspaceSlug, kbId, summary)
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
              {tab.label} {tab.count}
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
                {tab.label} {tab.count}
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
