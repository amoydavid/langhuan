import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import { Braces, FileText, HardDrive, Workflow } from 'lucide-react'
import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { formatDateTime } from '@/lib/i18n/datetime'
import { DocumentStatusBadge } from './document-list'
import { documentPollInterval, useDocumentVisibility } from './polling'
import { documentQueryOptions } from './queries'
import type { Document, DocumentStatus } from './types'

function formatDate(value: string) {
  return formatDateTime(value, {
    dateStyle: 'medium',
    timeStyle: 'short',
  })
}

function formatBytes(value: number) {
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`
  return `${(value / (1024 * 1024)).toFixed(1)} MB`
}

type PollState = {
  updatedAt: number
  status?: DocumentStatus
  stableCount: number
}

export function DocumentDetail({ jobId }: { jobId?: string }) {
  const { workspaceSlug, documentId } = useParams({
    from: '/_authenticated/workspaces/$workspaceSlug/documents/$documentId',
  })
  const { t } = useTranslation()
  const visible = useDocumentVisibility()
  const queryClient = useQueryClient()
  const pollState = useRef<PollState>({ updatedAt: 0, stableCount: 0 })
  const wasVisible = useRef(visible)
  const options = documentQueryOptions(workspaceSlug, documentId)
  const { data: item } = useQuery({
    ...options,
    refetchInterval: (query) => {
      const data = query.state.data as Document | undefined
      if (!data) return false
      if (query.state.dataUpdatedAt !== pollState.current.updatedAt) {
        pollState.current = {
          updatedAt: query.state.dataUpdatedAt,
          status: data.status,
          stableCount:
            pollState.current.status === data.status
              ? pollState.current.stableCount + 1
              : 0,
        }
      }
      return documentPollInterval({
        status: data.status,
        stableCount: pollState.current.stableCount,
        visible,
      })
    },
  })

  useEffect(() => {
    if (visible && !wasVisible.current) {
      void queryClient.invalidateQueries({ queryKey: options.queryKey })
    }
    wasVisible.current = visible
  }, [options.queryKey, queryClient, visible])

  if (!item) return null

  return (
    <div className='space-y-6'>
      <div className='flex flex-col justify-between gap-4 sm:flex-row sm:items-start'>
        <div>
          <p className='page-eyebrow'>{t('documents.detail.eyebrow')}</p>
          <h1 className='font-semibold text-2xl tracking-tight'>
            {item.title}
          </h1>
          <div className='mt-3'>
            <DocumentStatusBadge status={item.status} />
          </div>
        </div>
        {jobId && (
          <Button variant='outline' asChild>
            <Link
              to='/workspaces/$workspaceSlug/jobs/$jobId'
              params={{ workspaceSlug, jobId }}
            >
              <Workflow />
              {t('documents.detail.viewJobButton')}
            </Link>
          </Button>
        )}
      </div>

      {item.error_message && (
        <Alert variant='destructive'>
          <AlertTitle>{t('documents.detail.failedTitle')}</AlertTitle>
          <AlertDescription>{item.error_message}</AlertDescription>
        </Alert>
      )}

      <Card>
        <CardHeader>
          <CardTitle className='flex items-center gap-2'>
            <FileText className='size-4' />
            {t('documents.detail.fileInfoTitle')}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <dl className='grid gap-5 text-sm sm:grid-cols-2 lg:grid-cols-3'>
            <div>
              <dt className='text-muted-foreground'>
                {t('documents.detail.fileTypeLabel')}
              </dt>
              <dd className='mt-1'>
                {item.active_revision?.file_type ||
                  t('documents.detail.notApplicable')}
              </dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>Content-Type</dt>
              <dd className='mt-1'>
                {item.active_revision?.content_type ||
                  t('documents.detail.notApplicable')}
              </dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>
                {t('documents.detail.fileSizeLabel')}
              </dt>
              <dd className='mt-1'>
                {formatBytes(item.active_revision?.size_bytes ?? 0)}
              </dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>
                {t('documents.detail.documentTypeLabel')}
              </dt>
              <dd className='mt-1'>{item.kind}</dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>
                {t('documents.detail.sourceTypeLabel')}
              </dt>
              <dd className='mt-1'>
                {item.source_type || t('documents.detail.unknown')}
              </dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>
                {t('documents.detail.createdAtLabel')}
              </dt>
              <dd className='mt-1'>{formatDate(item.created_at)}</dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>
                {t('documents.detail.updatedAtLabel')}
              </dt>
              <dd className='mt-1'>{formatDate(item.updated_at)}</dd>
            </div>
          </dl>
        </CardContent>
      </Card>

      {item.normalized_markdown && (
        <Card>
          <CardHeader>
            <CardTitle>Normalized Markdown</CardTitle>
          </CardHeader>
          <CardContent>
            <pre className='code-panel max-h-[48rem] whitespace-pre-wrap'>
              {item.normalized_markdown}
            </pre>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle className='flex items-center gap-2'>
            <Braces className='size-4' />
            Metadata
          </CardTitle>
        </CardHeader>
        <CardContent>
          <pre className='code-panel'>
            {JSON.stringify(item.metadata, null, 2)}
          </pre>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className='flex items-center gap-2'>
            <HardDrive className='size-4' />
            {t('documents.detail.advancedInfoTitle')}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <dl className='grid gap-5 text-sm sm:grid-cols-2'>
            <div>
              <dt className='text-muted-foreground'>Document ID</dt>
              <dd className='mt-1 break-all font-mono'>{item.id}</dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>Knowledge Base ID</dt>
              <dd className='mt-1 break-all font-mono'>
                {item.knowledge_base_id}
              </dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>SHA256</dt>
              <dd className='mt-1 break-all font-mono'>
                {item.active_revision?.sha256 ||
                  t('documents.detail.notRecorded')}
              </dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>Active Revision ID</dt>
              <dd className='mt-1 break-all font-mono'>
                {item.active_revision?.id || t('documents.detail.notPublished')}
              </dd>
            </div>
          </dl>
        </CardContent>
      </Card>
    </div>
  )
}
