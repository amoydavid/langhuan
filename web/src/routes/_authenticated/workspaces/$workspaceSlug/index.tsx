import { createFileRoute } from '@tanstack/react-router'
import { Main } from '@/components/layout/main'
import { knowledgeBasesQueryOptions } from '@/features/knowledge-bases/queries'
import { workspaceReadinessQueryOptions } from '@/features/workspace-readiness/queries'
import { WorkspaceOverview } from '@/features/workspaces/workspace-overview'

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/'
)({
  loader: ({ context, params }) =>
    Promise.all([
      context.queryClient.ensureQueryData(
        workspaceReadinessQueryOptions(params.workspaceSlug)
      ),
      context.queryClient.ensureQueryData(
        knowledgeBasesQueryOptions(params.workspaceSlug)
      ),
    ]),
  staticData: {
    breadcrumb: { label: 'routes.workspaces.overview.breadcrumb' },
  },
  component: () => (
    <Main>
      <WorkspaceOverview />
    </Main>
  ),
})
