import { ArrowRight, CircleAlert, FileText } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { StatusBadge } from '@/components/status-badge'
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
import type { InsightsDocumentKind, SleepingDocument } from '../types'

type SleepingDocumentsProps = {
  workspaceSlug: string
  kbId: string
  insufficientSample: boolean
  documents: SleepingDocument[]
}

/** 沉睡行「查看」跳既有内容详情（与 content-list 的 canonical 路径同构）。 */
export function canonicalSleepingHref(
  workspaceSlug: string,
  kbId: string,
  item: SleepingDocument
) {
  const segment = item.kind === 'file' ? 'files' : item.kind
  return `/workspaces/${encodeURIComponent(workspaceSlug)}/kb/${encodeURIComponent(kbId)}/content/${segment}/${encodeURIComponent(item.document_id)}`
}

/**
 * 沉睡内容面板（spec §9.3.3）：固定 30 天口径；样本不足时只出提示块不出表
 * （防止零流量知识库把全部内容误报为沉睡）；不提供删除入口（删除走内容侧预警流）。
 */
export function SleepingDocuments({
  workspaceSlug,
  kbId,
  insufficientSample,
  documents,
}: SleepingDocumentsProps) {
  const { t } = useTranslation()
  const kindLabel: Record<InsightsDocumentKind, string> = {
    file: t('insights.sleeping.kind.file'),
    web: t('insights.sleeping.kind.web'),
    faq: t('insights.sleeping.kind.faq'),
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-base'>
          {t('insights.sleeping.title')}
        </CardTitle>
        <CardDescription>{t('insights.sleeping.description')}</CardDescription>
      </CardHeader>
      <CardContent>
        {insufficientSample ? (
          <div className='flex items-start gap-3 rounded-lg bg-muted/45 p-4'>
            <CircleAlert className='mt-0.5 size-4 shrink-0 text-primary' />
            <div>
              <p className='font-medium text-sm'>
                {t('insights.sleeping.insufficientTitle')}
              </p>
              <p className='mt-1 text-muted-foreground text-sm leading-6'>
                {t('insights.sleeping.insufficientDescription')}
              </p>
            </div>
          </div>
        ) : documents.length === 0 ? (
          <div className='flex min-h-32 flex-col items-center justify-center rounded-lg border border-dashed p-6 text-center'>
            <FileText className='mb-3 size-6 text-muted-foreground' />
            <p className='font-medium text-sm'>
              {t('insights.sleeping.emptyTitle')}
            </p>
            <p className='mt-1 text-muted-foreground text-sm'>
              {t('insights.sleeping.emptyHint')}
            </p>
          </div>
        ) : (
          <div className='overflow-hidden rounded-xl border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('insights.sleeping.columnName')}</TableHead>
                  <TableHead>{t('insights.sleeping.columnKind')}</TableHead>
                  <TableHead>{t('insights.sleeping.columnStatus')}</TableHead>
                  <TableHead>{t('insights.sleeping.columnIndexdAt')}</TableHead>
                  <TableHead className='w-14'>
                    {t('insights.sleeping.columnActions')}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {documents.map((item) => (
                  <TableRow key={item.document_id}>
                    <TableCell className='font-medium'>{item.title}</TableCell>
                    <TableCell>{kindLabel[item.kind]}</TableCell>
                    <TableCell>
                      <StatusBadge tone='success'>
                        {t('insights.sleeping.statusReady')}
                      </StatusBadge>
                    </TableCell>
                    <TableCell className='text-muted-foreground text-sm'>
                      {formatDateTime(item.indexed_at, { dateStyle: 'short' })}
                    </TableCell>
                    <TableCell>
                      <Button variant='ghost' size='icon' asChild>
                        <a
                          href={canonicalSleepingHref(
                            workspaceSlug,
                            kbId,
                            item
                          )}
                          aria-label={t('insights.sleeping.viewAriaLabel', {
                            name: item.title,
                          })}
                        >
                          <ArrowRight />
                        </a>
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
