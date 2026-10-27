import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ArrowRight, Globe2, Loader2, RefreshCw, RotateCw } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { parseApiError } from '@/lib/api/error'
import i18n from '@/lib/i18n'
import { formatDateTime } from '@/lib/i18n/datetime'
import { readWebSourceConfig, webSourceHealth } from '../config'
import {
  syncWebSourceMutationOptions,
  updateWebSourceMutationOptions,
} from '../queries'
import type { WebSourceView } from '../types'
import { ForceRecrawlDialog } from './force-recrawl-dialog'
import { HealthBadge } from './health-badge'

type WebSourceCardProps = {
  view: WebSourceView
  workspaceSlug: string
  kbId: string
  canManage: boolean
}

const summaryPartKeys = [
  'webSource.card.lastSyncSynced',
  'webSource.card.lastSyncDeleted',
  'webSource.card.lastSyncFailed',
] as const

/** 上次同步结果摘要（同步/删除/失败仅显示非零项）；无摘要返回 null。 */
export function lastSyncSummary(
  result: WebSourceView['source']['sync_last_result']
): string | null {
  const counts = [
    result.synced_documents,
    result.deleted_documents,
    result.failed_documents,
  ]
  const parts = counts
    .map((count, index) =>
      (count ?? 0) > 0
        ? i18n.t(summaryPartKeys[index], { count: count ?? 0 })
        : null
    )
    .filter((part): part is string => part !== null)
  return parts.length > 0 ? parts.join(' · ') : null
}

/** 列表页源卡片（spec §9.5.1）：只保留「立即同步」与「进入」，破坏性操作收进详情页。 */
export function WebSourceCard({
  view,
  workspaceSlug,
  kbId,
  canManage,
}: WebSourceCardProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const config = readWebSourceConfig(view.source.config)
  const health = webSourceHealth(view.source)
  const [forceRecrawlOpen, setForceRecrawlOpen] = useState(false)
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
  const lastResult = view.source.sync_last_result

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

  /** 强制重爬（v1.4.0 §9.3.7）：忽略变更检测全量重抓，仅在确认对话框后触发。 */
  async function handleForceRecrawl() {
    try {
      const result = await syncMutation.mutateAsync(true)
      toast.success(t('webSource.card.syncQueuedToast'), {
        description: result.job_id,
      })
    } catch (error) {
      toast.error(parseApiError(error).message)
      throw error
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
    <Card data-testid='web-source-card'>
      <CardHeader>
        <div className='flex min-w-0 items-center gap-2'>
          <span className='icon-tile flex size-8 shrink-0 items-center justify-center'>
            <Globe2 className='size-4' />
          </span>
          <CardTitle className='truncate text-base'>
            <Link
              to='/workspaces/$workspaceSlug/kb/$kbId/content/web/sources/$sourceId'
              params={{ workspaceSlug, kbId, sourceId: view.source.id }}
              className='hover:underline'
            >
              {view.source.name}
            </Link>
          </CardTitle>
          <Badge variant='secondary'>
            {t(`webSource.mode.${config.mode}`)}
          </Badge>
          <CardAction>
            <HealthBadge health={health} />
          </CardAction>
        </div>
      </CardHeader>
      <CardContent className='space-y-1.5 text-muted-foreground text-sm'>
        <p className='truncate'>
          {config.mode === 'crawl' && (
            <>
              {t('webSource.card.seedLabel')}{' '}
              <span className='font-mono text-xs'>{config.seedURL}</span>
            </>
          )}
          {config.mode === 'sitemap' && (
            <>
              {t('webSource.card.sitemapLabel')}{' '}
              <span className='font-mono text-xs'>{config.sitemapURL}</span>
            </>
          )}
          {config.mode === 'urls' &&
            t('webSource.card.urlCount', { count: config.urls.length })}
          {config.engine !== 'http' && (
            <>
              {' · '}
              {t(`webSource.engine.${config.engine}`)}
            </>
          )}
          {' · '}
          {t(
            config.respectRobots
              ? 'webSource.detail.respectRobotsOn'
              : 'webSource.detail.respectRobotsOff'
          )}
        </p>
        <p className='truncate'>
          {view.failed_page_count > 0
            ? t('webSource.card.pageCountWithFailed', {
                count: view.page_count,
                failed: view.failed_page_count,
              })
            : t('webSource.card.pageCount', { count: view.page_count })}
          {' · '}
          {lastResult.finished_at && (
            <>
              {t('webSource.card.lastSyncAt', {
                time: formatDateTime(lastResult.finished_at, {
                  dateStyle: 'short',
                  timeStyle: 'short',
                }),
              })}
              {'：'}
            </>
          )}
          {lastSyncSummary(lastResult) ??
            (lastResult.status === 'failed'
              ? t('webSource.card.lastSyncFailedOnly')
              : t('webSource.card.lastSyncNone'))}
        </p>
        <p>
          {view.source.status === 'active' && view.source.next_sync_at
            ? t('webSource.card.nextSyncAt', {
                time: formatDateTime(view.source.next_sync_at, {
                  dateStyle: 'short',
                  timeStyle: 'short',
                }),
              })
            : t('webSource.card.nextSyncPaused')}
        </p>
        <div className='flex flex-wrap items-center gap-2 pt-2'>
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
          {canManage && view.source.status === 'active' && (
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={syncMutation.isPending}
              onClick={() => setForceRecrawlOpen(true)}
            >
              <RotateCw />
              {t('webSource.forceRecrawl.button')}
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
              {toggleMutation.isPending ? (
                <Loader2 className='animate-spin' />
              ) : null}
              {t('webSource.card.enable')}
            </Button>
          )}
          <Button type='button' variant='ghost' size='sm' asChild>
            <Link
              to='/workspaces/$workspaceSlug/kb/$kbId/content/web/sources/$sourceId'
              params={{ workspaceSlug, kbId, sourceId: view.source.id }}
            >
              {t('webSource.card.enter')}
              <ArrowRight />
            </Link>
          </Button>
        </div>
      </CardContent>
      {canManage && view.source.status === 'active' && (
        <ForceRecrawlDialog
          open={forceRecrawlOpen}
          onOpenChange={setForceRecrawlOpen}
          pageCount={view.page_count}
          onConfirm={handleForceRecrawl}
        />
      )}
    </Card>
  )
}
