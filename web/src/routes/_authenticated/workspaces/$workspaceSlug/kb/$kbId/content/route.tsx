import { useQuery } from '@tanstack/react-query'
import { createFileRoute, useParams } from '@tanstack/react-router'
import { ContentLayout } from '@/features/content/content-layout'
import { knowledgeBaseSummaryQueryOptions } from '@/features/knowledge-bases/workbench/queries'

function KnowledgeBaseContentRoute() {
  const { workspaceSlug, kbId } = useParams({
    from: '/_authenticated/workspaces/$workspaceSlug/kb/$kbId/content',
  })
  const { data: summary } = useQuery(
    knowledgeBaseSummaryQueryOptions(workspaceSlug, kbId)
  )
  if (!summary) return null
  return (
    <ContentLayout
      workspaceSlug={workspaceSlug}
      kbId={kbId}
      summary={summary}
    />
  )
}

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/kb/$kbId/content'
)({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(
      knowledgeBaseSummaryQueryOptions(params.workspaceSlug, params.kbId)
    ),
  staticData: {
    breadcrumb: { label: 'routes.workspaces.kb.content.breadcrumb' },
  },
  component: KnowledgeBaseContentRoute,
})
