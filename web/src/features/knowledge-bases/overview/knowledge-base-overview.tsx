import {
  AlertTriangle,
  ArrowRight,
  Boxes,
  CheckCircle2,
  Clock3,
  FileText,
  Search,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import type { JobStatus } from '@/features/jobs/types'
import type { KnowledgeBaseSummary } from '@/features/knowledge-bases/workbench/types'
import { formatDateTime } from '@/lib/i18n/datetime'

type KnowledgeBaseOverviewProps = {
  workspaceSlug: string
  kbId: string
  summary: KnowledgeBaseSummary
  canManageIndex: boolean
}

function formatDate(value: string) {
  return formatDateTime(value, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function KnowledgeBaseOverview({
  workspaceSlug,
  kbId,
  summary,
  canManageIndex,
}: KnowledgeBaseOverviewProps) {
  const { t } = useTranslation()
  const jobStatusLabel: Record<JobStatus, string> = {
    pending: t('knowledgeBases.overview.jobStatus.pending'),
    queued: t('knowledgeBases.overview.jobStatus.queued'),
    running: t('knowledgeBases.overview.jobStatus.running'),
    completed: t('knowledgeBases.overview.jobStatus.completed'),
    succeeded: t('knowledgeBases.overview.jobStatus.succeeded'),
    failed: t('knowledgeBases.overview.jobStatus.failed'),
    cancelled: t('knowledgeBases.overview.jobStatus.cancelled'),
  }
  const base = `/workspaces/${encodeURIComponent(workspaceSlug)}/kb/${encodeURIComponent(kbId)}`
  const counts = summary.document_counts
  const active = summary.active_generation

  const countCards = [
    {
      label: t('knowledgeBases.overview.countTotal'),
      value: counts.total,
      href: `${base}/content/all`,
    },
    {
      label: t('knowledgeBases.overview.countReady'),
      value: counts.ready,
      href: `${base}/content/all?status=ready`,
    },
    {
      label: t('knowledgeBases.overview.countProcessing'),
      value: counts.processing,
      href: `${base}/content/all?status=processing`,
    },
    {
      label: t('knowledgeBases.overview.countFailed'),
      value: counts.failed,
      href: `${base}/content/all?status=failed`,
    },
  ]

  return (
    <div className='space-y-5'>
      <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
        {countCards.map((item) => (
          <a key={item.label} href={item.href} className='group'>
            <Card className='h-full transition-colors group-hover:border-primary/30'>
              <CardHeader className='gap-1 pb-3'>
                <CardDescription>{item.label}</CardDescription>
                <CardTitle className='text-2xl'>{item.value}</CardTitle>
              </CardHeader>
            </Card>
          </a>
        ))}
      </div>

      <div className='grid gap-5 xl:grid-cols-2'>
        <Card>
          <CardHeader>
            <CardTitle className='flex items-center gap-2 text-base'>
              <Boxes className='size-4 text-primary' />
              {t('knowledgeBases.overview.activeIndexTitle')}
            </CardTitle>
          </CardHeader>
          <CardContent>
            {active ? (
              <div className='space-y-3'>
                <p className='font-medium text-sm'>{active.display_label}</p>
                <p className='text-muted-foreground text-sm'>
                  {t('knowledgeBases.overview.activeModelLine', {
                    modelName: active.model_display_name,
                    dimensions: active.embedding_dimension,
                  })}
                </p>
                <dl className='grid gap-3 rounded-lg bg-muted/45 p-4 text-sm sm:grid-cols-2'>
                  <div>
                    <dt className='text-muted-foreground'>
                      {t('knowledgeBases.overview.syncLabel')}
                    </dt>
                    <dd className='mt-1 flex flex-wrap gap-x-3 font-medium'>
                      <span>
                        {t('knowledgeBases.overview.contentVersion', {
                          version: summary.content_version,
                        })}
                      </span>
                      <span>
                        {t('knowledgeBases.overview.indexedVersion', {
                          version: active.indexed_content_version,
                        })}
                      </span>
                    </dd>
                  </div>
                  <div>
                    <dt className='text-muted-foreground'>
                      {t('knowledgeBases.overview.scaleLabel')}
                    </dt>
                    <dd className='mt-1 font-medium'>
                      {t('knowledgeBases.overview.documentScale', {
                        documentCount: active.document_count,
                        chunkCount: active.chunk_count,
                      })}
                    </dd>
                  </div>
                </dl>
              </div>
            ) : (
              <div className='rounded-lg border border-destructive/30 bg-destructive/5 p-4'>
                <p className='font-medium text-sm'>
                  {t('knowledgeBases.overview.missingActiveTitle')}
                </p>
                <p className='mt-1 text-muted-foreground text-sm'>
                  {t('knowledgeBases.overview.missingActiveDescription')}
                </p>
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className='flex items-center gap-2 text-base'>
              <AlertTriangle className='size-4 text-primary' />
              {t('knowledgeBases.overview.blockersTitle')}
            </CardTitle>
          </CardHeader>
          <CardContent>
            {summary.blockers.length === 0 ? (
              <div className='flex items-start gap-3 rounded-lg bg-muted/45 p-4'>
                <CheckCircle2 className='mt-0.5 size-4 text-primary' />
                <div>
                  <p className='font-medium text-sm'>
                    {t('knowledgeBases.overview.noBlockersTitle')}
                  </p>
                  <p className='mt-1 text-muted-foreground text-sm'>
                    {t('knowledgeBases.overview.noBlockersDescription')}
                  </p>
                </div>
              </div>
            ) : (
              <ul className='divide-y divide-border'>
                {summary.blockers.map((blocker) => (
                  <li
                    key={`${blocker.code}-${blocker.resource_id}`}
                    className='py-3 first:pt-0 last:pb-0'
                  >
                    <div className='flex items-start gap-3'>
                      <AlertTriangle className='mt-0.5 size-4 shrink-0 text-destructive' />
                      <div className='min-w-0'>
                        <p className='font-medium text-sm'>
                          {blocker.resource_display_name}
                        </p>
                        <p className='mt-1 text-muted-foreground text-sm leading-6'>
                          {blocker.message}
                        </p>
                      </div>
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader className='flex-row items-center justify-between'>
          <div>
            <CardTitle className='flex items-center gap-2 text-base'>
              <Clock3 className='size-4 text-primary' />
              {t('knowledgeBases.overview.recentJobsTitle')}
            </CardTitle>
            <CardDescription className='mt-1'>
              {t('knowledgeBases.overview.recentJobsDescription')}
            </CardDescription>
          </div>
        </CardHeader>
        <CardContent>
          {summary.recent_jobs.length === 0 ? (
            <p className='rounded-lg border border-dashed p-6 text-center text-muted-foreground text-sm'>
              {t('knowledgeBases.overview.noRecentJobs')}
            </p>
          ) : (
            <ul className='divide-y divide-border'>
              {summary.recent_jobs.map((job) => (
                <li
                  key={job.id}
                  className='flex flex-col gap-2 py-3 first:pt-0 last:pb-0 sm:flex-row sm:items-center sm:justify-between'
                >
                  <div className='min-w-0'>
                    <p className='truncate font-medium text-sm'>
                      {job.action_label} · {job.target_display_name}
                    </p>
                    {job.error_message && (
                      <p className='mt-1 truncate text-destructive text-xs'>
                        {job.error_message}
                      </p>
                    )}
                  </div>
                  <div className='flex shrink-0 items-center gap-2'>
                    <Badge
                      variant={
                        job.status === 'failed' ? 'destructive' : 'secondary'
                      }
                    >
                      {jobStatusLabel[job.status]}
                    </Badge>
                    <span className='text-muted-foreground text-xs'>
                      {formatDate(job.updated_at)}
                    </span>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <div className='flex flex-wrap gap-2'>
        <Button asChild>
          <a href={`${base}/content/all`}>
            <FileText />
            {t('knowledgeBases.overview.addContentButton')}
          </a>
        </Button>
        <Button variant='outline' asChild>
          <a href={`${base}/search`}>
            <Search />
            {t('knowledgeBases.overview.searchTestButton')}
          </a>
        </Button>
        {canManageIndex && (
          <Button variant='outline' asChild>
            <a href={`${base}/indexes?create=true`}>
              <Boxes />
              {t('knowledgeBases.overview.buildIndexButton')}
            </a>
          </Button>
        )}
        {summary.candidate_generation && (
          <Button variant='ghost' asChild>
            <a href={`${base}/indexes`}>
              {t('knowledgeBases.overview.checkCandidateButton')}
              <ArrowRight />
            </a>
          </Button>
        )}
      </div>
    </div>
  )
}
