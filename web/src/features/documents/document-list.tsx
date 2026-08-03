import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ArrowRight, FileText, Plus } from 'lucide-react'
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
import { documentsQueryOptions } from './queries'
import type { DocumentStatus } from './types'

export function DocumentStatusBadge({ status }: { status: DocumentStatus }) {
  const { t } = useTranslation()
  const statusMeta = {
    pending: { label: t('documents.list.status.pending'), tone: 'warning' },
    processing: { label: t('documents.list.status.processing'), tone: 'info' },
    ready: { label: t('documents.list.status.ready'), tone: 'success' },
    failed: { label: t('documents.list.status.failed'), tone: 'danger' },
    deleting: { label: t('documents.list.status.deleting'), tone: 'info' },
    deleted: { label: t('documents.list.status.deleted'), tone: 'neutral' },
  } as const satisfies Record<
    DocumentStatus,
    {
      label: string
      tone: 'success' | 'warning' | 'danger' | 'info' | 'neutral'
    }
  >
  const meta = statusMeta[status]
  return <StatusBadge tone={meta.tone}>{meta.label}</StatusBadge>
}

type DocumentListProps = {
  workspaceSlug: string
  kbId: string
}

export function DocumentList({ workspaceSlug, kbId }: DocumentListProps) {
  const { t } = useTranslation()
  const { data: documents = [] } = useQuery(
    documentsQueryOptions(workspaceSlug, kbId)
  )

  return (
    <Card>
      <CardHeader>
        <div>
          <CardTitle>{t('documents.list.title')}</CardTitle>
          <CardDescription className='mt-2'>
            {t('documents.list.description')}
          </CardDescription>
        </div>
        <div className='col-start-2 row-span-2 row-start-1 self-start justify-self-end'>
          <Button asChild size='sm'>
            <Link
              to='/workspaces/$workspaceSlug/kb/$kbId/documents/new'
              params={{ workspaceSlug, kbId }}
            >
              <Plus />
              {t('documents.list.uploadButton')}
            </Link>
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        {documents.length === 0 ? (
          <div className='flex min-h-40 flex-col items-center justify-center rounded-lg border border-dashed text-center'>
            <FileText className='mb-3 size-6 text-muted-foreground' />
            <p className='font-medium text-sm'>
              {t('documents.list.emptyTitle')}
            </p>
            <p className='mt-1 text-muted-foreground text-sm'>
              {t('documents.list.emptyDescription')}
            </p>
          </div>
        ) : (
          <div className='overflow-hidden rounded-lg border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('documents.list.columnTitle')}</TableHead>
                  <TableHead>{t('documents.list.columnStatus')}</TableHead>
                  <TableHead>{t('documents.list.columnUpdatedAt')}</TableHead>
                  <TableHead className='w-12'>
                    {t('documents.list.columnActions')}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {documents.map((item) => (
                  <TableRow key={item.id}>
                    <TableCell>
                      <div className='font-medium'>{item.title}</div>
                      <div className='mt-1 text-muted-foreground text-xs'>
                        {item.active_revision?.file_type || item.kind}
                      </div>
                    </TableCell>
                    <TableCell>
                      <DocumentStatusBadge status={item.status} />
                    </TableCell>
                    <TableCell className='text-muted-foreground text-sm'>
                      {formatDateTime(item.updated_at, {
                        dateStyle: 'short',
                        timeStyle: 'short',
                      })}
                    </TableCell>
                    <TableCell>
                      <Button variant='ghost' size='icon' asChild>
                        <Link
                          to='/workspaces/$workspaceSlug/documents/$documentId'
                          params={{ workspaceSlug, documentId: item.id }}
                          search={{}}
                          aria-label={t('documents.list.viewAriaLabel', {
                            title: item.title,
                          })}
                        >
                          <ArrowRight />
                        </Link>
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
