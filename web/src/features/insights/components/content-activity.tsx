import { Link } from '@tanstack/react-router'
import { FilePenLine, RefreshCw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import i18n from '@/lib/i18n'
import { formatDateTime } from '@/lib/i18n/datetime'
import type { InsightsActivityEvent } from '../types'

type ContentActivityProps = {
  workspaceSlug: string
  kbId: string
  events: InsightsActivityEvent[]
  limit: number
}

// revision_reason → 文案 key（spec §4.4）：ingest/replace=上传新版本、crawl=抓取更新、
// edit=编辑内容、reparse=重新解析；未知 reason 用 fallback 兜底。
const revisionReasonKeys: Record<string, string> = {
  ingest: 'insights.activity.revisionReason.ingest',
  replace: 'insights.activity.revisionReason.replace',
  crawl: 'insights.activity.revisionReason.crawl',
  edit: 'insights.activity.revisionReason.edit',
  reparse: 'insights.activity.revisionReason.reparse',
}

/** 事件行文案（纯函数，i18n 组装）：sync=「源名 同步成功/失败」、revision=「标题 动作」、delete=「标题 已移出检索」。 */
export function activityEventTitle(event: InsightsActivityEvent): string {
  if (event.kind === 'sync') {
    const name = event.source_name ?? event.document_title ?? ''
    return i18n.t(
      event.success
        ? 'insights.activity.syncSuccess'
        : 'insights.activity.syncFailed',
      { name }
    )
  }
  if (event.kind === 'revision') {
    const reasonKey =
      (event.revision_reason && revisionReasonKeys[event.revision_reason]) ||
      'insights.activity.revisionReason.fallback'
    return i18n.t('insights.activity.revisionLine', {
      title: event.document_title ?? '',
      // 动态 key 来自本文件的受控 Record，用 as never 走既有逃生（见 localizedErrorMessage）。
      reason: i18n.t(reasonKey as never),
    })
  }
  return i18n.t('insights.activity.deleteLine', {
    title: event.document_title ?? '',
  })
}

/** 行点击目标：sync → web 源列表；revision/delete → 文档详情（缺身份时回内容列表）。 */
export function activityEventHref(
  event: InsightsActivityEvent,
  workspaceSlug: string,
  kbId: string
): string {
  const base = `/workspaces/${encodeURIComponent(workspaceSlug)}/kb/${encodeURIComponent(kbId)}`
  if (event.kind === 'sync') return `${base}/content/web`
  if (event.target_document_id && event.document_kind) {
    const segment =
      event.document_kind === 'file' ? 'files' : event.document_kind
    return `${base}/content/${segment}/${encodeURIComponent(event.target_document_id)}`
  }
  return `${base}/content/all`
}

const kindIcons = {
  sync: RefreshCw,
  revision: FilePenLine,
  delete: Trash2,
} as const

/**
 * 内容动态 feed（spec §9.3.4）：同步/新版本/移除统一时间线，行范式复用
 * Overview「最近任务」（左事件文本 + 右 Badge + 灰字时间，divide-y）。
 */
export function ContentActivity({
  workspaceSlug,
  kbId,
  events,
  limit,
}: ContentActivityProps) {
  const { t } = useTranslation()
  const formatTime = (value: string) =>
    formatDateTime(value, {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    })

  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-base'>
          {t('insights.activity.title')}
        </CardTitle>
        <CardDescription>
          {t('insights.activity.description', { count: limit })}
        </CardDescription>
      </CardHeader>
      <CardContent>
        {events.length === 0 ? (
          <p className='rounded-lg border border-dashed p-6 text-center text-muted-foreground text-sm'>
            {t('insights.activity.empty')}
          </p>
        ) : (
          <ul className='divide-y divide-border'>
            {events.map((event, index) => {
              const Icon = kindIcons[event.kind]
              const badgeLabel = t(
                event.kind === 'sync'
                  ? 'insights.activity.badgeSync'
                  : event.kind === 'revision'
                    ? 'insights.activity.badgeRevision'
                    : 'insights.activity.badgeDelete'
              )
              return (
                <li key={`${event.kind}-${event.occurred_at}-${index}`}>
                  <Link
                    to={activityEventHref(event, workspaceSlug, kbId)}
                    className='flex flex-col gap-2 py-3 transition-colors first:pt-0 last:pb-0 hover:text-primary sm:flex-row sm:items-center sm:justify-between'
                  >
                    <div className='flex min-w-0 items-start gap-2'>
                      <Icon className='mt-0.5 size-4 shrink-0 text-primary' />
                      <div className='min-w-0'>
                        <p className='truncate font-medium text-sm'>
                          {activityEventTitle(event)}
                        </p>
                        {!event.success && event.error_message && (
                          <p className='mt-1 truncate text-destructive text-xs'>
                            {event.error_message}
                          </p>
                        )}
                      </div>
                    </div>
                    <div className='flex shrink-0 items-center gap-2'>
                      <Badge
                        variant={
                          event.kind === 'sync' && !event.success
                            ? 'destructive'
                            : 'secondary'
                        }
                      >
                        {badgeLabel}
                      </Badge>
                      <span className='text-muted-foreground text-xs'>
                        {formatTime(event.occurred_at)}
                      </span>
                    </div>
                  </Link>
                </li>
              )
            })}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}
