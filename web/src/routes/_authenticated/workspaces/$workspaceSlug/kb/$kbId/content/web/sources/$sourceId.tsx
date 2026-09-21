import { useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { meQueryOptions } from '@/features/auth/queries'
import { WebSourceDetail } from '@/features/content/web-source/components/web-source-detail'
import { webSourceQueryOptions } from '@/features/content/web-source/queries'
import { canManageSources } from '@/features/knowledge-bases/permissions'

function WebSourceDetailPage() {
  const { workspaceSlug, kbId, sourceId } = Route.useParams()
  // 写操作门控与列表页同口径：owner/admin（服务端 RequireAdminForSession 对齐）。
  const { data: me } = useQuery(meQueryOptions())
  const role = me?.workspaces.find((item) => item.slug === workspaceSlug)?.role
  return (
    <div className='min-w-0'>
      <WebSourceDetail
        workspaceSlug={workspaceSlug}
        kbId={kbId}
        sourceId={sourceId}
        canManage={canManageSources(role)}
      />
    </div>
  )
}

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/kb/$kbId/content/web/sources/$sourceId'
)({
  loader: async ({ context, params }) => {
    const view = await context.queryClient.ensureQueryData(
      webSourceQueryOptions(params.workspaceSlug, params.kbId, params.sourceId)
    )
    return { sourceName: view.source.name }
  },
  staticData: {
    breadcrumb: {
      label: 'routes.workspaces.kb.content.web.sources.breadcrumb',
      resolve: (data: unknown) =>
        typeof data === 'object' &&
        data !== null &&
        'sourceName' in data &&
        typeof data.sourceName === 'string'
          ? data.sourceName
          : undefined,
    },
  },
  component: WebSourceDetailPage,
})
