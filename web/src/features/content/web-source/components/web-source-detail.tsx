import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import {
  Loader2,
  Pencil,
  RefreshCw,
  RotateCw,
  Trash2,
  Wrench,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { ContentList } from '@/features/content/lists/content-list'
import { webSourceDocumentsQueryOptions } from '@/features/documents/queries'
import type { DocumentStatus } from '@/features/documents/types'
import { parseApiError } from '@/lib/api/error'
import { formatDateTime } from '@/lib/i18n/datetime'
import { readWebSourceConfig, webSourceHealth } from '../config'
import {
  syncWebSourceMutationOptions,
  updateWebSourceMutationOptions,
  webSourceQueryOptions,
} from '../queries'
import type { WebSourceView } from '../types'
import { DeleteWebSourceDialog } from './delete-web-source-dialog'
import { ForceRecrawlDialog } from './force-recrawl-dialog'
import { HealthBadge } from './health-badge'
import { ManageUrlsDialog } from './manage-urls-dialog'
import { WebSourceFormDialog } from './web-source-form-dialog'

type WebSourceDetailProps = {
  workspaceSlug: string
  kbId: string
  sourceId: string
  /** owner/admin 才渲染写操作（与列表页同口径）；成员只保留只读导航与页面列表。 */
  canManage: boolean
}

/** 源详情页主体（spec §9.5.2）：顶部源卡片 + 该源抓到的页面列表。 */
export function WebSourceDetail({
  workspaceSlug,
  kbId,
  sourceId,
  canManage,
}: WebSourceDetailProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { data: view, isPending } = useQuery(
    webSourceQueryOptions(workspaceSlug, kbId, sourceId)
  )
  const [editOpen, setEditOpen] = useState(false)
  const [manageOpen, setManageOpen] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)

  if (isPending) {
    return (
      <div className='space-y-4' aria-busy>
        <Skeleton className='h-8 w-40' />
        <Skeleton className='h-44 w-full rounded-xl' />
      </div>
    )
  }
  if (!view) return null
  const config = readWebSourceConfig(view.source.config)
  const configSummaryParts = [
    t(`webSource.engine.${config.engine}`),
    t(
      config.respectRobots
        ? 'webSource.detail.respectRobotsOn'
        : 'webSource.detail.respectRobotsOff'
    ),
    config.cron !== ''
      ? t('webSource.detail.cronOn', { cron: config.cron })
      : t('webSource.detail.cronOff'),
    t(
      config.onDelete === 'keep'
        ? 'webSource.detail.onDeleteKeep'
        : 'webSource.detail.onDeleteRemove'
    ),
  ]

  return (
    <div className='space-y-4'>
      {/* 源摘要条：紧凑单卡，与其它内容 tab 的列表头部风格一致；
          页间导航由面包屑与「Web」tab 承担，不再放返回按钮。 */}
      <section
        data-testid='web-source-detail-card'
        className='rounded-lg border bg-card'
      >
        <div className='flex flex-col gap-3 p-4'>
          <div className='flex flex-wrap items-center gap-2'>
            <h2 className='font-semibold text-lg'>{view.source.name}</h2>
            <HealthBadge health={webSourceHealth(view.source)} />
          </div>
          <div className='space-y-1 text-muted-foreground text-sm'>
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
            </p>
            <p>
              {t(`webSource.mode.${config.mode}`)}
              {' · '}
              {configSummaryParts.join(' · ')}
              {view.source.next_sync_at && (
                <>
                  {' · '}
                  {t('webSource.card.nextSyncAt', {
                    time: formatDateTime(view.source.next_sync_at, {
                      dateStyle: 'short',
                      timeStyle: 'short',
                    }),
                  })}
                </>
              )}
            </p>
          </div>
        </div>
        {canManage && (
          <DetailActions
            workspaceSlug={workspaceSlug}
            kbId={kbId}
            view={view}
            onEdit={() => setEditOpen(true)}
            onManageUrls={() => setManageOpen(true)}
            onDelete={() => setDeleteOpen(true)}
          />
        )}
      </section>

      <SourcePages
        workspaceSlug={workspaceSlug}
        kbId={kbId}
        sourceId={sourceId}
        pageCount={view.page_count}
      />

      {canManage && (
        <>
          <WebSourceFormDialog
            workspaceSlug={workspaceSlug}
            kbId={kbId}
            open={editOpen}
            onOpenChange={setEditOpen}
            source={view}
          />
          {config.mode === 'urls' && (
            <ManageUrlsDialog
              workspaceSlug={workspaceSlug}
              kbId={kbId}
              source={view}
              open={manageOpen}
              onOpenChange={setManageOpen}
            />
          )}
          <DeleteWebSourceDialog
            workspaceSlug={workspaceSlug}
            kbId={kbId}
            source={view}
            open={deleteOpen}
            onOpenChange={setDeleteOpen}
            onDeleted={() =>
              void navigate({
                to: '/workspaces/$workspaceSlug/kb/$kbId/content/web',
                params: { workspaceSlug, kbId },
              })
            }
          />
        </>
      )}
    </div>
  )
}

type DetailActionsProps = {
  workspaceSlug: string
  kbId: string
  view: WebSourceView
  onEdit: () => void
  onManageUrls: () => void
  onDelete: () => void
}

function DetailActions({
  workspaceSlug,
  kbId,
  view,
  onEdit,
  onManageUrls,
  onDelete,
}: DetailActionsProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const config = readWebSourceConfig(view.source.config)
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
  const isActive = view.source.status === 'active'

  async function handleSync() {
    try {
      const result = await syncMutation.mutateAsync(false)
      toast.success(t('webSource.detail.syncQueuedToast'), {
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
      toast.success(t('webSource.detail.syncQueuedToast'), {
        description: result.job_id,
      })
    } catch (error) {
      toast.error(parseApiError(error).message)
      throw error
    }
  }

  async function handleToggle() {
    try {
      await toggleMutation.mutateAsync({
        status: isActive ? 'disabled' : 'active',
      })
      toast.success(t('webSource.detail.statusChangedToast'))
    } catch (error) {
      toast.error(parseApiError(error).message)
    }
  }

  return (
    <div className='flex flex-wrap items-center gap-2 border-t px-4 py-3'>
      {isActive && (
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
          {t('webSource.detail.syncNow')}
        </Button>
      )}
      {isActive && (
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
      <Button type='button' variant='outline' size='sm' onClick={onEdit}>
        <Pencil />
        {t('webSource.detail.editButton')}
      </Button>
      {config.mode === 'urls' && (
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={onManageUrls}
        >
          <Wrench />
          {t('webSource.detail.manageUrlsButton')}
        </Button>
      )}
      <Button
        type='button'
        variant='outline'
        size='sm'
        disabled={toggleMutation.isPending}
        onClick={() => void handleToggle()}
      >
        {toggleMutation.isPending ? <Loader2 className='animate-spin' /> : null}
        {t(
          isActive
            ? 'webSource.detail.disableButton'
            : 'webSource.detail.enableButton'
        )}
      </Button>
      <Button
        type='button'
        variant='ghost'
        size='sm'
        className='text-destructive hover:text-destructive'
        onClick={onDelete}
      >
        <Trash2 />
        {t('webSource.detail.deleteButton')}
      </Button>
      {isActive && (
        <ForceRecrawlDialog
          open={forceRecrawlOpen}
          onOpenChange={setForceRecrawlOpen}
          pageCount={view.page_count}
          onConfirm={handleForceRecrawl}
        />
      )}
    </div>
  )
}

type SourcePagesProps = {
  workspaceSlug: string
  kbId: string
  sourceId: string
  pageCount: number
}

/** 该源抓到的页面列表 = ContentList kind=web 按 kb_source_id 过滤（列结构零新增）。 */
function SourcePages({
  workspaceSlug,
  kbId,
  sourceId,
  pageCount,
}: SourcePagesProps) {
  const { t } = useTranslation()
  const { data: documents = [] } = useQuery(
    webSourceDocumentsQueryOptions(workspaceSlug, kbId, sourceId)
  )
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState<DocumentStatus | undefined>(undefined)

  return (
    <div className='space-y-3'>
      <div className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
        <h3 className='font-medium text-base'>
          {t('webSource.detail.pagesTitle', { count: pageCount })}
        </h3>
        <div className='flex flex-col gap-2 sm:flex-row'>
          <Input
            value={query}
            placeholder={t(
              'routes.workspaces.kb.content.web.searchPlaceholder'
            )}
            aria-label={t('routes.workspaces.kb.content.web.searchAriaLabel')}
            className='sm:max-w-xs'
            onChange={(event) => setQuery(event.target.value)}
          />
          <Select
            value={status ?? 'all'}
            onValueChange={(value) =>
              setStatus(value === 'all' ? undefined : (value as DocumentStatus))
            }
          >
            <SelectTrigger
              aria-label={t('routes.workspaces.kb.content.web.statusAriaLabel')}
              className='sm:w-36'
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='all'>
                {t('routes.workspaces.kb.content.web.statusAll')}
              </SelectItem>
              <SelectItem value='ready'>
                {t('routes.workspaces.kb.content.web.statusReady')}
              </SelectItem>
              <SelectItem value='processing'>
                {t('routes.workspaces.kb.content.web.statusProcessing')}
              </SelectItem>
              <SelectItem value='failed'>
                {t('routes.workspaces.kb.content.web.statusFailed')}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>
      <ContentList
        workspaceSlug={workspaceSlug}
        kbId={kbId}
        documents={documents}
        kind='web'
        query={query}
        status={status}
      />
    </div>
  )
}
