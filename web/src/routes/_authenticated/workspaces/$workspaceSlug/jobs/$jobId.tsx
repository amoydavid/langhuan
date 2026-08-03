import { createFileRoute } from '@tanstack/react-router'
import type { RouteBreadcrumb } from '@/components/layout/app-breadcrumbs'
import { Main } from '@/components/layout/main'
import { JobDetail } from '@/features/documents/job-detail'
import { jobQueryOptions } from '@/features/documents/queries'

function jobType(loaderData: unknown) {
  if (
    typeof loaderData === 'object' &&
    loaderData !== null &&
    'type' in loaderData &&
    typeof loaderData.type === 'string'
  ) {
    return loaderData.type
  }
  return undefined
}

const breadcrumb: RouteBreadcrumb = {
  label: 'routes.workspaces.jobs.detail.breadcrumb',
  resolve: jobType,
}

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/jobs/$jobId'
)({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(
      jobQueryOptions(params.workspaceSlug, params.jobId)
    ),
  staticData: { breadcrumb },
  component: () => (
    <Main>
      <JobDetail />
    </Main>
  ),
})
