import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ArrowRight, Globe2, Loader2, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { parseApiError } from '@/lib/api/error'
import { formatDateTime } from '@/lib/i18n/datetime'
import { readWebSourceConfig, webSourceHealth } from '../config'
import {
  syncWebSourceMutationOptions,
  updateWebSourceMutationOptions,
} from '../queries'
import type { WebSourceView } from '../types'
import { HealthBadge } from './health-badge'
import { lastSyncSummary } from './web-source-card'

type WebSourceTableProps = {
  views: WebSourceView[]
  workspaceSlug: string
  kbId: string
  canManage: boolean
}

/** 源表格行：立即同步（可管理且 active）+ 进入链接。 */
function WebSourceTableRow({
  view,
  workspaceSlug,
  kbId,
  canManage,
}: {
  view: WebSourceView
  workspaceSlug: string
  kbId: string
  canManage: boolean
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const config = readWebSourceConfig(view.source.config)
  const health = webSourceHealth(view.source)
  const lastResult = view.source.sync_last_result
  const syncMutation = useMutation(
    syncWebSourceMutationOptions(
      workspaceSlug,
      kbId,
      view.source.id,
      queryClient
    )
  )
  const toggleMutation = useMutation(
    updateWebSourceMutationOptions(
      workspaceSlug,
      kbId,
      view.source.id,
      queryClient
    )
  )

  async function handleSync() {
    try {
      const result = await syncMutation.mutateAsync(false)
      toast.success(t('webSource.card.syncQueuedToast'), {
        description: result.job_id,
      })
    } catch (error) {
      toast.error(parseApiError(error).message)
    }
  }

  async function handleEnable() {
    try {
      await toggleMutation.mutateAsync({ status: 'active' })
      toast.success(t('webSource.detail.statusChangedToast'))
    } catch (error) {
      toast.error(parseApiError(error).message)
    }
  }

  return (
    <TableRow data-testid='web-source-row'>
      <TableCell>
        <div className='flex min-w-0 items-center gap-2 font-medium'>
          <Globe2 className='size-4 shrink-0 text-primary' />
          <Link
            to='/workspaces/$workspaceSlug/kb/$kbId/content/web/sources/$sourceId'
            params={{ workspaceSlug, kbId, sourceId: view.source.id }}
            className='truncate hover:underline'
          >
            {view.source.name}
          </Link>
          <Badge variant='secondary'>
            {t(`webSource.mode.${config.mode}`)}
          </Badge>
        </div>
      </TableCell>
      <TableCell className='max-w-72 truncate text-muted-foreground text-sm'>
        <span className='font-mono text-xs'>
          {config.mode === 'crawl' && config.seedURL}
          {config.mode === 'sitemap' && config.sitemapURL}
          {config.mode === 'urls' &&
            t('webSource.card.urlCount', { count: config.urls.length })}
        </span>
        {' · '}
        {config.engine !== 'http' &&
          `${t(`webSource.engine.${config.engine}`)} · `}
        {t(
          config.respectRobots
            ? 'webSource.detail.respectRobotsOn'
            : 'webSource.detail.respectRobotsOff'
        )}
      </TableCell>
      <TableCell>
        <HealthBadge health={health} />
      </TableCell>
      <TableCell className='text-sm'>
        {view.failed_page_count > 0
          ? t('webSource.card.pageCountWithFailed', {
              count: view.page_count,
              failed: view.failed_page_count,
            })
          : t('webSource.card.pageCount', { count: view.page_count })}
      </TableCell>
      <TableCell className='text-muted-foreground text-sm'>
        {lastResult.finished_at
          ? formatDateTime(lastResult.finished_at, {
              dateStyle: 'short',
              timeStyle: 'short',
            })
          : '—'}
        {lastSyncSummary(lastResult) && (
          <span className='block text-xs'>{lastSyncSummary(lastResult)}</span>
        )}
      </TableCell>
      <TableCell>
        <div className='flex items-center gap-1.5'>
          {canManage && view.source.status === 'active' && (
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={syncMutation.isPending}
              onClick={() => void handleSync()}
            >
              {syncMutation.isPending ? (
                <Loader2 className='animate-spin' />
              ) : (
                <RefreshCw />
              )}
              {t('webSource.card.syncNow')}
            </Button>
          )}
          {canManage && view.source.status === 'disabled' && (
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={toggleMutation.isPending}
              onClick={() => void handleEnable()}
            >
              {t('webSource.list.enableButton')}
            </Button>
          )}
          <Button variant='ghost' size='icon' asChild>
            <Link
              to='/workspaces/$workspaceSlug/kb/$kbId/content/web/sources/$sourceId'
              params={{ workspaceSlug, kbId, sourceId: view.source.id }}
              aria-label={t('webSource.list.enterAriaLabel', {
                name: view.source.name,
              })}
            >
              <ArrowRight />
            </Link>
          </Button>
        </div>
      </TableCell>
    </TableRow>
  )
}

/** PC 端源表格（与其它内容 tab 的表格式展示一致，spec §9.5.1）。 */
export function WebSourceTable({
  views,
  workspaceSlug,
  kbId,
  canManage,
}: WebSourceTableProps) {
  const { t } = useTranslation()
  return (
    <div className='overflow-hidden rounded-xl border'>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('webSource.list.columnName')}</TableHead>
            <TableHead>{t('webSource.list.columnSummary')}</TableHead>
            <TableHead>{t('webSource.list.columnStatus')}</TableHead>
            <TableHead>{t('webSource.list.columnPages')}</TableHead>
            <TableHead>{t('webSource.list.columnLastSync')}</TableHead>
            <TableHead className='w-28'>
              {t('webSource.list.columnActions')}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {views.map((view) => (
            <WebSourceTableRow
              key={view.source.id}
              view={view}
              workspaceSlug={workspaceSlug}
              kbId={kbId}
              canManage={canManage}
            />
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
