import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { Workflow } from 'lucide-react'
import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { StatusBadge } from '@/components/status-badge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { formatDateTime } from '@/lib/i18n/datetime'
import { jobPollInterval, useDocumentVisibility } from './polling'
import { jobQueryOptions } from './queries'
import type { Job, JobStatus } from './types'

function JobStatusBadge({ status }: { status: JobStatus }) {
  const { t } = useTranslation()
  const statusMeta = {
    queued: { label: t('documents.job.status.queued'), tone: 'warning' },
    running: { label: t('documents.job.status.running'), tone: 'info' },
    succeeded: { label: t('documents.job.status.succeeded'), tone: 'success' },
    failed: { label: t('documents.job.status.failed'), tone: 'danger' },
    cancelled: { label: t('documents.job.status.cancelled'), tone: 'neutral' },
  } as const satisfies Record<
    JobStatus,
    {
      label: string
      tone: 'success' | 'warning' | 'danger' | 'info' | 'neutral'
    }
  >
  const meta = statusMeta[status]
  return <StatusBadge tone={meta.tone}>{meta.label}</StatusBadge>
}

function formatDate(value: string) {
  return formatDateTime(value, {
    dateStyle: 'medium',
    timeStyle: 'short',
  })
}

export function JobDetail() {
  const { workspaceSlug, jobId } = useParams({
    from: '/_authenticated/workspaces/$workspaceSlug/jobs/$jobId',
  })
  const { t } = useTranslation()
  const visible = useDocumentVisibility()
  const queryClient = useQueryClient()
  const wasVisible = useRef(visible)
  const pollState = useRef({
    updatedAt: 0,
    status: undefined as JobStatus | undefined,
    stableCount: 0,
  })
  const options = jobQueryOptions(workspaceSlug, jobId)
  const { data: job } = useQuery({
    ...options,
    refetchInterval: (query) => {
      const data = query.state.data as Job | undefined
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
      return jobPollInterval({
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

  if (!job) return null

  return (
    <div className='space-y-6'>
      <div>
        <p className='page-eyebrow'>{t('documents.job.eyebrow')}</p>
        <h1 className='font-semibold text-2xl tracking-tight'>{job.type}</h1>
        <div className='mt-3'>
          <JobStatusBadge status={job.status} />
        </div>
      </div>

      {job.error_message && (
        <Alert variant='destructive'>
          <AlertTitle>{t('documents.job.failedTitle')}</AlertTitle>
          <AlertDescription>{job.error_message}</AlertDescription>
        </Alert>
      )}

      <Card>
        <CardHeader>
          <CardTitle className='flex items-center gap-2'>
            <Workflow className='size-4' />
            {t('documents.job.infoTitle')}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <dl className='grid gap-5 text-sm sm:grid-cols-2 lg:grid-cols-3'>
            <div>
              <dt className='text-muted-foreground'>
                {t('documents.job.attemptsLabel')}
              </dt>
              <dd className='mt-1'>{job.attempts}</dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>External job ID</dt>
              <dd className='mt-1 break-all font-mono'>
                {job.external_job_id || t('documents.job.notRecorded')}
              </dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>Document ID</dt>
              <dd className='mt-1 break-all font-mono'>{job.document_id}</dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>
                {t('documents.job.createdAtLabel')}
              </dt>
              <dd className='mt-1'>{formatDate(job.created_at)}</dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>
                {t('documents.job.updatedAtLabel')}
              </dt>
              <dd className='mt-1'>{formatDate(job.updated_at)}</dd>
            </div>
            <div>
              <dt className='text-muted-foreground'>Job ID</dt>
              <dd className='mt-1 break-all font-mono'>{job.id}</dd>
            </div>
          </dl>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Payload</CardTitle>
        </CardHeader>
        <CardContent>
          <pre className='code-panel max-h-[40rem]'>
            {JSON.stringify(job.payload, null, 2)}
          </pre>
        </CardContent>
      </Card>
    </div>
  )
}
