import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { SearchX, ShieldAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { parseApiError } from '@/lib/api/error'
import { INSIGHTS_ACTIVITY_LIMIT, INSIGHTS_SLEEPING_LIMIT } from '../api'
import {
  insightsActivityQueryOptions,
  insightsOverviewQueryOptions,
  sleepingDocumentsQueryOptions,
  updateQueryLogSettingsMutationOptions,
  zeroRecallQueriesQueryOptions,
} from '../queries'
import { INSIGHTS_WINDOW_DAYS, type InsightsWindowDays } from '../types'
import { ContentActivity } from './content-activity'
import { InsightCards } from './insight-cards'
import { SleepingDocuments } from './sleeping-documents'
import { ZeroRecallQueries } from './zero-recall-queries'

type InsightsPageProps = {
  workspaceSlug: string
  kbId: string
  windowDays: InsightsWindowDays
  /** 窗口切换由路由层执行（写入 URL），页面只负责展示。 */
  onWindowChange: (windowDays: InsightsWindowDays) => void
}

/** member 无权限态（spec §9.4）：只出说明，不请求任何数据。 */
export function InsightsMemberNotice() {
  const { t } = useTranslation()
  return (
    <div className='flex min-h-64 flex-col items-center justify-center rounded-xl border border-dashed bg-muted/20 text-center'>
      <ShieldAlert className='mb-3 size-6 text-muted-foreground' />
      <p className='font-medium text-sm'>{t('insights.member.title')}</p>
      <p className='mt-1 max-w-md text-muted-foreground text-sm leading-6'>
        {t('insights.member.description')}
      </p>
    </div>
  )
}

function InsightsSkeleton() {
  return (
    <div className='space-y-5' data-testid='insights-skeleton'>
      <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
        {Array.from({ length: 4 }, (_, index) => (
          <Skeleton key={`insights-card-${index}`} className='h-28' />
        ))}
      </div>
      <Skeleton className='h-64' />
    </div>
  )
}

/**
 * 健康 tab 主页面：四张计数卡 + 零召回/沉睡/内容动态三个面板。
 * 窗口（7/14/30）作用于计数卡与零召回；沉睡固定 30 天口径（spec §9.2）。
 */
export function InsightsPage({
  workspaceSlug,
  kbId,
  windowDays,
  onWindowChange,
}: InsightsPageProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const windowLabels: Record<InsightsWindowDays, string> = {
    7: t('insights.page.window7'),
    14: t('insights.page.window14'),
    30: t('insights.page.window30'),
  }

  const overviewQuery = useQuery(
    insightsOverviewQueryOptions(workspaceSlug, kbId, windowDays)
  )
  const queriesQuery = useQuery(
    zeroRecallQueriesQueryOptions(workspaceSlug, kbId, windowDays)
  )
  const documentsQuery = useQuery(
    sleepingDocumentsQueryOptions(workspaceSlug, kbId, INSIGHTS_SLEEPING_LIMIT)
  )
  const activityQuery = useQuery(
    insightsActivityQueryOptions(workspaceSlug, kbId, INSIGHTS_ACTIVITY_LIMIT)
  )
  const settingsMutation = useMutation(
    updateQueryLogSettingsMutationOptions(workspaceSlug, kbId, queryClient)
  )

  async function handleEnableQueryLog(): Promise<boolean> {
    try {
      await settingsMutation.mutateAsync(true)
      toast.success(t('insights.zeroRecall.enableDialog.successToast'))
      return true
    } catch (error) {
      toast.error(parseApiError(error).message)
      return false
    }
  }

  // 角色缓存过期（降权后 me 未刷新）时端点会 403：按权限说明态展示，不暴露数据。
  if (
    overviewQuery.error &&
    parseApiError(overviewQuery.error).status === 403
  ) {
    return <InsightsMemberNotice />
  }

  const overview = overviewQuery.data
  const activity = activityQuery.data
  if (!overview) return <InsightsSkeleton />

  // 全空态（spec §9.4）：窗口内零 run 且无内容变更；activity 未就绪前不判空，避免闪现。
  const isActivitySettled = activityQuery.isSuccess || activityQuery.isError
  if (
    overview.runs_total === 0 &&
    isActivitySettled &&
    (activity?.events.length ?? 0) === 0
  ) {
    return (
      <div className='flex min-h-64 flex-col items-center justify-center rounded-xl border border-dashed bg-muted/20 text-center'>
        <SearchX className='mb-3 size-6 text-muted-foreground' />
        <p className='font-medium text-sm'>{t('insights.empty.title')}</p>
        <p className='mt-1 max-w-md text-muted-foreground text-sm leading-6'>
          {t('insights.empty.description')}
        </p>
      </div>
    )
  }

  const queryLogEnabled =
    queriesQuery.data?.query_log_enabled ?? overview.query_log_enabled

  return (
    <div className='space-y-5'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <p className='text-muted-foreground text-sm'>
          {t('insights.page.scopeNote')}
        </p>
        <Select
          value={String(windowDays)}
          onValueChange={(value) =>
            onWindowChange(Number(value) as InsightsWindowDays)
          }
        >
          <SelectTrigger
            className='w-40'
            aria-label={t('insights.page.windowAriaLabel')}
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {INSIGHTS_WINDOW_DAYS.map((days) => (
              <SelectItem key={days} value={String(days)}>
                {windowLabels[days]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <InsightCards overview={overview} windowDays={windowDays} />

      <ZeroRecallQueries
        workspaceSlug={workspaceSlug}
        kbId={kbId}
        windowDays={windowDays}
        queryLogEnabled={queryLogEnabled}
        queries={queriesQuery.data?.queries ?? []}
        onEnableQueryLog={handleEnableQueryLog}
        enablePending={settingsMutation.isPending}
      />

      <SleepingDocuments
        workspaceSlug={workspaceSlug}
        kbId={kbId}
        insufficientSample={
          documentsQuery.data?.insufficient_sample ??
          overview.sleeping_insufficient_sample
        }
        documents={documentsQuery.data?.documents ?? []}
      />

      <ContentActivity
        workspaceSlug={workspaceSlug}
        kbId={kbId}
        events={activity?.events ?? []}
        limit={INSIGHTS_ACTIVITY_LIMIT}
      />
    </div>
  )
}
