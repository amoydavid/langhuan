import { useQuery } from '@tanstack/react-query'
import { createFileRoute, notFound, useParams } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import type { RouteBreadcrumb } from '@/components/layout/app-breadcrumbs'
import { Skeleton } from '@/components/ui/skeleton'
import { meQueryOptions } from '@/features/auth/queries'
import { knowledgeBaseQueryOptions } from '@/features/knowledge-bases/queries'
import { knowledgeBaseSummaryQueryOptions } from '@/features/knowledge-bases/workbench/queries'
import { KnowledgeBaseWorkbenchLayout } from '@/features/knowledge-bases/workbench/workbench-layout'
import { parseApiError } from '@/lib/api/error'
import i18n from '@/lib/i18n'

type KnowledgeBaseLoaderData = { knowledgeBaseName: string }

function knowledgeBaseName(loaderData: unknown) {
  if (
    typeof loaderData === 'object' &&
    loaderData !== null &&
    'knowledgeBaseName' in loaderData &&
    typeof loaderData.knowledgeBaseName === 'string'
  ) {
    return loaderData.knowledgeBaseName
  }
  return undefined
}

const breadcrumb: RouteBreadcrumb = {
  label: 'routes.workspaces.kb.detail.breadcrumb',
  resolve: knowledgeBaseName,
}

function WorkbenchRouteSkeleton() {
  const { t } = useTranslation()
  return (
    <div
      className='space-y-5'
      role='status'
      aria-label={t('routes.workspaces.kb.detail.loading')}
    >
      <div className='space-y-2 border-b pb-5'>
        <Skeleton className='h-8 w-52' />
        <Skeleton className='h-4 w-96 max-w-full' />
        <Skeleton className='h-9 w-full max-w-xl' />
      </div>
      <div className='grid gap-4 sm:grid-cols-2 xl:grid-cols-4'>
        {Array.from({ length: 4 }, (_, index) => (
          <Skeleton key={`workbench-card-${index + 1}`} className='h-28' />
        ))}
      </div>
    </div>
  )
}

function KnowledgeBaseWorkbenchRoute() {
  const { workspaceSlug, kbId } = useParams({
    from: '/_authenticated/workspaces/$workspaceSlug/kb/$kbId',
  })
  const { data: knowledgeBase } = useQuery(
    knowledgeBaseQueryOptions(workspaceSlug, kbId)
  )
  const { data: summary } = useQuery(
    knowledgeBaseSummaryQueryOptions(workspaceSlug, kbId)
  )

  if (!knowledgeBase || !summary) return <WorkbenchRouteSkeleton />

  return (
    <KnowledgeBaseWorkbenchLayout
      workspaceSlug={workspaceSlug}
      kbId={kbId}
      knowledgeBase={knowledgeBase}
      summary={summary}
    />
  )
}

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/kb/$kbId'
)({
  loader: async ({ context, params }): Promise<KnowledgeBaseLoaderData> => {
    try {
      const [knowledgeBase] = await Promise.all([
        context.queryClient.ensureQueryData(
          knowledgeBaseQueryOptions(params.workspaceSlug, params.kbId)
        ),
        context.queryClient.ensureQueryData(
          knowledgeBaseSummaryQueryOptions(params.workspaceSlug, params.kbId)
        ),
        context.queryClient.ensureQueryData(meQueryOptions()),
      ])
      return { knowledgeBaseName: knowledgeBase.name }
    } catch (error) {
      if (parseApiError(error).status === 404) throw notFound()
      throw error
    }
  },
  staticData: { breadcrumb },
  pendingComponent: WorkbenchRouteSkeleton,
  notFoundComponent: () => (
    <div className='mx-auto flex min-h-80 max-w-lg flex-col items-center justify-center text-center'>
      <p className='font-medium text-primary text-sm'>404</p>
      <h1 className='mt-2 font-semibold text-2xl'>
        {i18n.t('routes.workspaces.kb.detail.notFoundTitle')}
      </h1>
      <p className='mt-2 text-muted-foreground'>
        {i18n.t('routes.workspaces.kb.detail.notFoundDescription')}
      </p>
    </div>
  ),
  component: KnowledgeBaseWorkbenchRoute,
})
