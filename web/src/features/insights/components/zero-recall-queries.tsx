import { CircleCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatDateTime } from '@/lib/i18n/datetime'
import type { ZeroRecallQuery } from '../types'
import { QueryLogAlert } from './query-log-alert'

type ZeroRecallQueriesProps = {
  workspaceSlug: string
  kbId: string
  windowDays: number
  queryLogEnabled: boolean
  queries: ZeroRecallQuery[]
  /** 确认开启查询原文记录；resolve true=成功（由容器执行 PUT 并失效缓存）。 */
  onEnableQueryLog: () => Promise<boolean>
  enablePending?: boolean
}

function formatSeen(value: string) {
  return formatDateTime(value, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function formatFirstSeen(value: string) {
  return formatDateTime(value, { dateStyle: 'short' })
}

/**
 * 零召回查询面板（spec §9.3.1/§9.3.2）两态：
 * 开启原文记录 → 明文表格 + 创建 FAQ/检索测试 行动；
 * 未开启 → 脱敏行（•••+字数/次数/首末时间）+ 提示条与开启确认。
 */
export function ZeroRecallQueries({
  workspaceSlug,
  kbId,
  windowDays,
  queryLogEnabled,
  queries,
  onEnableQueryLog,
  enablePending,
}: ZeroRecallQueriesProps) {
  const { t } = useTranslation()
  const base = `/workspaces/${encodeURIComponent(workspaceSlug)}/kb/${encodeURIComponent(kbId)}`

  if (queries.length === 0) {
    // 面板级空态是好状态（spec §9.4）；未开启记录时仍保留提示条与开启入口。
    return (
      <Card>
        <CardHeader>
          <CardTitle className='text-base'>
            {t('insights.zeroRecall.title')}
          </CardTitle>
          <CardDescription>
            {t('insights.zeroRecall.description', { window: windowDays })}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {!queryLogEnabled && (
            <QueryLogAlert
              onEnable={onEnableQueryLog}
              enablePending={enablePending}
            />
          )}
          <div className='flex min-h-32 flex-col items-center justify-center rounded-lg border border-dashed p-6 text-center'>
            <p className='flex items-center gap-2 font-medium text-sm'>
              <CircleCheck className='size-4 text-success' />
              {t('insights.zeroRecall.emptyTitle', { window: windowDays })}
            </p>
            <p className='mt-1 text-muted-foreground text-sm'>
              {t('insights.zeroRecall.emptyHint')}
            </p>
          </div>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-base'>
          {t('insights.zeroRecall.title')}
        </CardTitle>
        <CardDescription>
          {t('insights.zeroRecall.description', { window: windowDays })}
        </CardDescription>
      </CardHeader>
      <CardContent>
        {!queryLogEnabled && (
          <QueryLogAlert
            onEnable={onEnableQueryLog}
            enablePending={enablePending}
          />
        )}

        <div className='overflow-hidden rounded-xl border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>
                  {t(
                    queryLogEnabled
                      ? 'insights.zeroRecall.columnQuery'
                      : 'insights.zeroRecall.columnMasked'
                  )}
                </TableHead>
                <TableHead>{t('insights.zeroRecall.columnHits')}</TableHead>
                {!queryLogEnabled && (
                  <TableHead>
                    {t('insights.zeroRecall.columnFirstSeen')}
                  </TableHead>
                )}
                <TableHead>{t('insights.zeroRecall.columnLastSeen')}</TableHead>
                {queryLogEnabled && (
                  <TableHead>
                    {t('insights.zeroRecall.columnActions')}
                  </TableHead>
                )}
              </TableRow>
            </TableHeader>
            <TableBody>
              {queries.map((item) => (
                <TableRow key={item.query_hash}>
                  <TableCell className='font-medium'>
                    {queryLogEnabled && item.latest_query_text
                      ? item.latest_query_text
                      : t('insights.zeroRecall.maskedQuery', {
                          chars: item.query_chars,
                        })}
                  </TableCell>
                  <TableCell>{item.hits}</TableCell>
                  {!queryLogEnabled && (
                    <TableCell className='text-muted-foreground text-sm'>
                      {formatFirstSeen(item.first_seen_at)}
                    </TableCell>
                  )}
                  <TableCell className='text-muted-foreground text-sm'>
                    {formatSeen(item.last_seen_at)}
                  </TableCell>
                  {queryLogEnabled && (
                    <TableCell>
                      {item.latest_query_text ? (
                        <div className='flex items-center gap-1'>
                          <Button variant='outline' size='sm' asChild>
                            <a
                              href={`${base}/content/faq/new?question=${encodeURIComponent(item.latest_query_text)}`}
                            >
                              {t('insights.zeroRecall.createFaq')}
                            </a>
                          </Button>
                          <Button variant='ghost' size='sm' asChild>
                            <a
                              href={`${base}/search?q=${encodeURIComponent(item.latest_query_text)}`}
                            >
                              {t('insights.zeroRecall.testSearch')}
                            </a>
                          </Button>
                        </div>
                      ) : (
                        // 开启记录前的查询无原文：第 5 列仍渲染，置禁用态避免死链。
                        <Button
                          variant='outline'
                          size='sm'
                          disabled
                          title={t('insights.zeroRecall.noOriginalTitle')}
                        >
                          {t('insights.zeroRecall.noOriginal')}
                        </Button>
                      )}
                    </TableCell>
                  )}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </CardContent>
    </Card>
  )
}
