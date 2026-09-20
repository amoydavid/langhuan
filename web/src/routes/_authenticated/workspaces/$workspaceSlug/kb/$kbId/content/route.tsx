import { useQuery } from '@tanstack/react-query'
import { createFileRoute, useParams } from '@tanstack/react-router'
import { ContentLayout } from '@/features/content/content-layout'
import { webSourcesQueryOptions } from '@/features/content/web-source/queries'
import { knowledgeBaseSummaryQueryOptions } from '@/features/knowledge-bases/workbench/queries'

function KnowledgeBaseContentRoute() {
  const { workspaceSlug, kbId } = useParams({
    from: '/_authenticated/workspaces/$workspaceSlug/kb/$kbId/content',
  })
  const { data: summary } = useQuery(
    knowledgeBaseSummaryQueryOptions(workspaceSlug, kbId)
  )
  // 「Web」子 tab 显示条件需要源信息（存在 web 源即可见，spec §9.4）。
  const { data: webSources } = useQuery(
    webSourcesQueryOptions(workspaceSlug, kbId)
  )
  if (!summary) return null
  return (
    <ContentLayout
      workspaceSlug={workspaceSlug}
      kbId={kbId}
      summary={summary}
      webSourceCount={webSources?.length}
    />
  )
}

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/kb/$kbId/content'
)({
  loader: ({ context, params }) =>
    Promise.all([
      context.queryClient.ensureQueryData(
        knowledgeBaseSummaryQueryOptions(params.workspaceSlug, params.kbId)
      ),
      // 源列表为空是最常见情形，预取一次以避免 tab 闪烁。
      context.queryClient.ensureQueryData(
        webSourcesQueryOptions(params.workspaceSlug, params.kbId)
      ),
    ]),
  staticData: {
    breadcrumb: { label: 'routes.workspaces.kb.content.breadcrumb' },
  },
  component: KnowledgeBaseContentRoute,
})
