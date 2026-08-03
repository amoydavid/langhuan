import { useQuery } from '@tanstack/react-query'
import { createFileRoute, useParams } from '@tanstack/react-router'
import { meQueryOptions } from '@/features/auth/queries'
import { KnowledgeBaseOverview } from '@/features/knowledge-bases/overview/knowledge-base-overview'
import { canManageIndex } from '@/features/knowledge-bases/permissions'
import { knowledgeBaseSummaryQueryOptions } from '@/features/knowledge-bases/workbench/queries'

function KnowledgeBaseOverviewRoute() {
  const { workspaceSlug, kbId } = useParams({
    from: '/_authenticated/workspaces/$workspaceSlug/kb/$kbId/',
  })
  const { data: summary } = useQuery(
    knowledgeBaseSummaryQueryOptions(workspaceSlug, kbId)
  )
  const { data: me } = useQuery(meQueryOptions())
  const role = me?.workspaces.find((item) => item.slug === workspaceSlug)?.role

  if (!summary) return null

  return (
    <KnowledgeBaseOverview
      workspaceSlug={workspaceSlug}
      kbId={kbId}
      summary={summary}
      canManageIndex={canManageIndex(role)}
    />
  )
}

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/kb/$kbId/'
)({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(
      knowledgeBaseSummaryQueryOptions(params.workspaceSlug, params.kbId)
    ),
  component: KnowledgeBaseOverviewRoute,
})
