import { useQuery } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { Skeleton } from '@/components/ui/skeleton'
import { meQueryOptions } from '@/features/auth/queries'
import { knowledgeBasesQueryOptions } from '@/features/knowledge-bases/queries'
import { workspaceReadinessQueryOptions } from '@/features/workspace-readiness/queries'
import { WorkspaceReadinessPanel } from '@/features/workspace-readiness/workspace-readiness-panel'
import { workspaceQueryOptions } from './queries'

function WorkspaceOverviewSkeleton() {
  const { t } = useTranslation()
  return (
    <div
      className='space-y-5'
      role='status'
      aria-label={t('workspaces.overview.loadingLabel')}
    >
      <div className='space-y-2'>
        <Skeleton className='h-3 w-28' />
        <Skeleton className='h-8 w-52' />
        <Skeleton className='h-4 w-80 max-w-full' />
      </div>
      <Skeleton className='h-36 w-full rounded-xl' />
      <Skeleton className='h-80 w-full rounded-xl' />
      <div className='grid gap-5 lg:grid-cols-2'>
        <Skeleton className='h-64 w-full rounded-xl' />
        <Skeleton className='h-64 w-full rounded-xl' />
      </div>
    </div>
  )
}

export function WorkspaceOverview() {
  const { t } = useTranslation()
  const { workspaceSlug } = useParams({
    from: '/_authenticated/workspaces/$workspaceSlug/',
  })
  const { data: workspace } = useQuery(workspaceQueryOptions(workspaceSlug))
  const { data: me } = useQuery(meQueryOptions())
  const { data: readiness } = useQuery(
    workspaceReadinessQueryOptions(workspaceSlug)
  )
  const { data: knowledgeBases } = useQuery(
    knowledgeBasesQueryOptions(workspaceSlug)
  )
  const membership = me?.workspaces.find((item) => item.slug === workspaceSlug)

  if (!workspace || !readiness || !knowledgeBases || !membership) {
    return <WorkspaceOverviewSkeleton />
  }

  const canManageWorkspace =
    membership.role === 'owner' || membership.role === 'admin'

  return (
    <div className='space-y-6'>
      <div>
        <p className='page-eyebrow'>{t('workspaces.overview.eyebrow')}</p>
        <h1 className='font-semibold text-2xl tracking-tight'>
          {workspace.name}
        </h1>
        <p className='mt-2 max-w-2xl text-muted-foreground'>
          {t('workspaces.overview.description')}
        </p>
      </div>
      <WorkspaceReadinessPanel
        workspaceSlug={workspaceSlug}
        readiness={readiness}
        knowledgeBases={knowledgeBases}
        canManageWorkspace={canManageWorkspace}
        canManageInvitations={canManageWorkspace}
      />
    </div>
  )
}
