import { useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { meQueryOptions } from '@/features/auth/queries'
import { WebSourceList } from '@/features/content/web-source/components/web-source-list'
import { webSourcesQueryOptions } from '@/features/content/web-source/queries'
import { canManageSources } from '@/features/knowledge-bases/permissions'

function WebSourcesPage() {
  const { workspaceSlug, kbId } = Route.useParams()
  const { data: me } = useQuery(meQueryOptions())
  const role = me?.workspaces.find((item) => item.slug === workspaceSlug)?.role
  const canManage = canManageSources(role)
  return (
    <div className='mx-auto w-full max-w-4xl'>
      <WebSourceList
        workspaceSlug={workspaceSlug}
        kbId={kbId}
        canManage={canManage}
      />
    </div>
  )
}

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/kb/$kbId/content/web/'
)({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(
      webSourcesQueryOptions(params.workspaceSlug, params.kbId)
    ),
  staticData: {
    breadcrumb: {
      label: 'routes.workspaces.kb.content.web.breadcrumb',
    },
  },
  component: WebSourcesPage,
})
