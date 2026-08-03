import {
  ArrowRight,
  FileText,
  Globe2,
  MessageCircleQuestion,
} from 'lucide-react'
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
import type {
  Document,
  DocumentKind,
  DocumentStatus,
} from '@/features/documents/types'
import i18n from '@/lib/i18n'
import { formatDateTime } from '@/lib/i18n/datetime'

type ContentListProps = {
  workspaceSlug: string
  kbId: string
  documents: Document[]
  kind: DocumentKind | 'all'
  query?: string
  status?: DocumentStatus
}

function canonicalContentPath(
  workspaceSlug: string,
  kbId: string,
  item: Document
) {
  return `/workspaces/${encodeURIComponent(workspaceSlug)}/kb/${encodeURIComponent(kbId)}/content/${item.kind === 'file' ? 'files' : item.kind}/${encodeURIComponent(item.id)}`
}

function questionCount(item: Document) {
  return item.faq_question_count ?? 0
}

function formatUpdatedAt(value: string) {
  return formatDateTime(value, {
    dateStyle: 'short',
    timeStyle: 'short',
  })
}

function itemSummary(item: Document) {
  if (item.kind === 'faq') {
    return i18n.t('content.contentList.faqCount', {
      count: questionCount(item),
    })
  }
  if (item.kind === 'web') {
    return item.source_uri || i18n.t('content.contentList.noSourceUri')
  }
  return (
    item.active_revision?.file_type ||
    item.source_type ||
    i18n.t('content.contentList.fallbackType')
  )
}

export function ContentList({
  workspaceSlug,
  kbId,
  documents,
  kind,
  query = '',
  status,
}: ContentListProps) {
  const { t } = useTranslation()
  const kindMeta = {
    file: { label: t('content.contentList.kindFile'), icon: FileText },
    faq: {
      label: t('content.contentList.kindFaq'),
      icon: MessageCircleQuestion,
    },
    web: { label: t('content.contentList.kindWeb'), icon: Globe2 },
  } as const

  const statusMeta = {
    pending: { label: t('content.contentList.statusPending'), tone: 'warning' },
    processing: {
      label: t('content.contentList.statusProcessing'),
      tone: 'info',
    },
    ready: { label: t('content.contentList.statusReady'), tone: 'success' },
    failed: { label: t('content.contentList.statusFailed'), tone: 'danger' },
    deleting: {
      label: t('content.contentList.statusDeleting'),
      tone: 'info',
    },
    deleted: { label: t('content.contentList.statusDeleted'), tone: 'neutral' },
  } satisfies Record<
    DocumentStatus,
    {
      label: string
      tone: 'success' | 'warning' | 'danger' | 'info' | 'neutral'
    }
  >

  const normalizedQuery = query.trim().toLocaleLowerCase()
  const visible = documents
    .filter((item) => kind === 'all' || item.kind === kind)
    .filter((item) => !status || item.status === status)
    .filter(
      (item) =>
        normalizedQuery.length === 0 ||
        item.title.toLocaleLowerCase().includes(normalizedQuery)
    )

  if (visible.length === 0) {
    return (
      <div className='flex min-h-48 flex-col items-center justify-center rounded-xl border border-dashed bg-muted/20 text-center'>
        <FileText className='mb-3 size-6 text-muted-foreground' />
        <p className='font-medium text-sm'>
          {t('content.contentList.noResultsTitle')}
        </p>
        <p className='mt-1 text-muted-foreground text-sm'>
          {t('content.contentList.noResultsHint')}
        </p>
      </div>
    )
  }

  const label =
    kind === 'all' ? t('content.contentList.allLabel') : kindMeta[kind].label
  return (
    <>
      <div className='hidden overflow-hidden rounded-xl border md:block'>
        <Table aria-label={label}>
          <TableHeader>
            <TableRow>
              <TableHead>{t('content.contentList.columnName')}</TableHead>
              <TableHead>{t('content.contentList.columnType')}</TableHead>
              <TableHead>{t('content.contentList.columnSummary')}</TableHead>
              <TableHead>{t('content.contentList.columnStatus')}</TableHead>
              <TableHead>{t('content.contentList.columnUpdatedAt')}</TableHead>
              <TableHead className='w-14'>
                {t('content.contentList.columnActions')}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {visible.map((item) => {
              const meta = kindMeta[item.kind]
              const Icon = meta.icon
              return (
                <TableRow key={item.id}>
                  <TableCell>
                    <div className='flex items-center gap-2 font-medium'>
                      <Icon className='size-4 text-primary' />
                      {item.title || t('content.contentList.unnamed')}
                    </div>
                  </TableCell>
                  <TableCell>{meta.label}</TableCell>
                  <TableCell className='max-w-72 truncate text-muted-foreground'>
                    {itemSummary(item)}
                  </TableCell>
                  <TableCell>
                    <StatusBadge tone={statusMeta[item.status].tone}>
                      {statusMeta[item.status].label}
                    </StatusBadge>
                  </TableCell>
                  <TableCell className='text-muted-foreground text-sm'>
                    {formatUpdatedAt(item.updated_at)}
                  </TableCell>
                  <TableCell>
                    <Button variant='ghost' size='icon' asChild>
                      <a
                        href={canonicalContentPath(workspaceSlug, kbId, item)}
                        aria-label={t('content.contentList.viewAriaLabel', {
                          name: item.title || t('content.contentList.unnamed'),
                        })}
                      >
                        <ArrowRight />
                      </a>
                    </Button>
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      </div>

      <div data-testid='content-cards' className='grid gap-3 md:hidden'>
        {visible.map((item) => {
          const meta = kindMeta[item.kind]
          const Icon = meta.icon
          return (
            <a
              key={item.id}
              href={canonicalContentPath(workspaceSlug, kbId, item)}
              aria-label={t('content.contentList.viewAriaLabel', {
                name: item.title || t('content.contentList.unnamed'),
              })}
            >
              <Card className='transition-colors hover:border-primary/30'>
                <CardHeader className='gap-2'>
                  <div className='flex items-start justify-between gap-3'>
                    <CardTitle className='flex min-w-0 items-center gap-2 text-base'>
                      <Icon className='size-4 shrink-0 text-primary' />
                      <span className='truncate'>
                        {item.title || t('content.contentList.unnamed')}
                      </span>
                    </CardTitle>
                    <StatusBadge tone={statusMeta[item.status].tone}>
                      {statusMeta[item.status].label}
                    </StatusBadge>
                  </div>
                  <CardDescription>
                    {meta.label} · {itemSummary(item)}
                  </CardDescription>
                </CardHeader>
                <CardContent className='text-muted-foreground text-xs'>
                  {t('content.contentList.updatedOn', {
                    date: formatUpdatedAt(item.updated_at),
                  })}
                </CardContent>
              </Card>
            </a>
          )
        })}
      </div>
    </>
  )
}
