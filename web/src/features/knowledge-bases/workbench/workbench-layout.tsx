import {
  Link,
  Outlet,
  useLocation,
  useMatches,
  useNavigate,
} from '@tanstack/react-router'
import { ArrowRight, CircleAlert } from 'lucide-react'
import { type ReactNode, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { UploadFileDialog } from '@/features/content/file-tree/upload-file-dialog'
import type { KnowledgeBase } from '@/features/knowledge-bases/types'
import { cn } from '@/lib/utils'
import type { KnowledgeBaseSummary, KnowledgeBaseSyncState } from './types'

type KnowledgeBaseWorkbenchLayoutProps = {
  workspaceSlug: string
  kbId: string
  knowledgeBase: KnowledgeBase
  summary: KnowledgeBaseSummary
  children?: ReactNode
}

type WorkbenchTab = {
  label: string
  href: string
  segment: 'overview' | 'content' | 'search' | 'indexes' | 'settings'
}

function activeTab(pathname: string, tabs: WorkbenchTab[]) {
  return (
    tabs.find((tab) =>
      tab.segment === 'overview'
        ? pathname.replace(/\/$/, '') === tab.href
        : pathname.startsWith(tab.href.replace(/\/(all)$/, ''))
    ) ?? tabs[0]
  )
}

export function KnowledgeBaseWorkbenchLayout({
  workspaceSlug,
  kbId,
  knowledgeBase,
  summary,
  children,
}: KnowledgeBaseWorkbenchLayoutProps) {
  const { t } = useTranslation()
  const [uploadOpen, setUploadOpen] = useState(false)
  const location = useLocation()
  const navigate = useNavigate()
  const matches = useMatches()
  const fullHeight = matches.some((m) => m.staticData.fullHeight === true)
  const syncStateLabel: Record<KnowledgeBaseSyncState, string> = {
    synced: t('knowledgeBases.workbench.syncState.synced'),
    updating: t('knowledgeBases.workbench.syncState.updating'),
    failed: t('knowledgeBases.workbench.syncState.failed'),
    candidate_ready: t('knowledgeBases.workbench.syncState.candidate_ready'),
  }
  const base = `/workspaces/${encodeURIComponent(workspaceSlug)}/kb/${encodeURIComponent(kbId)}`
  const tabs: WorkbenchTab[] = [
    {
      label: t('knowledgeBases.workbench.tabs.overview'),
      href: base,
      segment: 'overview',
    },
    {
      label: t('knowledgeBases.workbench.tabs.content'),
      href: `${base}/content/all`,
      segment: 'content',
    },
    {
      label: t('knowledgeBases.workbench.tabs.search'),
      href: `${base}/search`,
      segment: 'search',
    },
    {
      label: t('knowledgeBases.workbench.tabs.indexes'),
      href: `${base}/indexes`,
      segment: 'indexes',
    },
    {
      label: t('knowledgeBases.workbench.tabs.settings'),
      href: `${base}/settings`,
      segment: 'settings',
    },
  ]
  const current = activeTab(location.pathname, tabs)

  return (
    <section
      data-testid='knowledge-base-workbench'
      className={cn(
        'space-y-4',
        fullHeight && 'flex h-full flex-col space-y-3 overflow-hidden'
      )}
    >
      <header className='shrink-0 border-b'>
        <div className='flex min-h-12 flex-col justify-between gap-2 py-2 sm:flex-row sm:items-center'>
          <div className='flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1'>
            <h1 className='truncate font-semibold text-xl tracking-tight'>
              {knowledgeBase.name || t('knowledgeBases.workbench.unnamedName')}
            </h1>
            <Badge
              variant={
                summary.sync_state === 'failed'
                  ? 'destructive'
                  : summary.sync_state === 'synced'
                    ? 'outline'
                    : 'secondary'
              }
            >
              {summary.sync_state === 'failed' && <CircleAlert />}
              {syncStateLabel[summary.sync_state]}
            </Badge>
            <span aria-hidden className='text-muted-foreground'>
              ·
            </span>
            <p className='text-muted-foreground text-sm'>
              {t('knowledgeBases.workbench.contentMeta', {
                version: summary.content_version,
                chunkCount: summary.active_generation?.chunk_count ?? 0,
              })}
            </p>
          </div>
          {(current.segment === 'overview' ||
            current.segment === 'content') && (
            <Button
              type='button'
              size='sm'
              className='shrink-0'
              onClick={() => setUploadOpen(true)}
            >
              {t('knowledgeBases.workbench.addContentButton')}
              <ArrowRight />
            </Button>
          )}
        </div>

        <nav
          aria-label={t('knowledgeBases.workbench.areaAriaLabel')}
          className='-mb-px hidden h-10 items-end gap-1 md:flex'
        >
          {tabs.map((tab) => {
            const selected = current.segment === tab.segment
            return (
              <Link
                key={tab.segment}
                to={tab.href}
                aria-current={selected ? 'page' : undefined}
                className={cn(
                  'rounded-t-md px-3 py-2 font-medium text-sm transition-colors',
                  selected
                    ? 'bg-primary/10 text-primary'
                    : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                )}
              >
                {tab.label}
              </Link>
            )
          })}
        </nav>

        <div className='pb-2 md:hidden'>
          <Select
            value={current.href}
            onValueChange={(href) => void navigate({ to: href })}
          >
            <SelectTrigger
              className='w-full'
              aria-label={t('knowledgeBases.workbench.areaAriaLabel')}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {tabs.map((tab) => (
                <SelectItem key={tab.segment} value={tab.href}>
                  {tab.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </header>

      <div
        className={cn('min-w-0', fullHeight && 'flex min-h-0 flex-1 flex-col')}
      >
        {children ?? <Outlet />}
      </div>
      <UploadFileDialog
        workspaceSlug={workspaceSlug}
        kbId={kbId}
        parentPath='/'
        open={uploadOpen}
        onOpenChange={setUploadOpen}
      />
    </section>
  )
}
