import { createFileRoute } from '@tanstack/react-router'
import { Main } from '@/components/layout/main'
import { meQueryOptions } from '@/features/auth/queries'
import { ModelProviderListPage } from '@/features/models/model-provider-list-page'
import { modelProvidersQueryOptions } from '@/features/models/queries'

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/models/'
)({
  loader: async ({ context, params }) => {
    const [me] = await Promise.all([
      context.queryClient.ensureQueryData(meQueryOptions()),
      context.queryClient.ensureQueryData(
        modelProvidersQueryOptions('workspace', params.workspaceSlug)
      ),
    ])
    return me
  },
  staticData: {
    breadcrumb: { label: 'routes.workspaces.models.breadcrumb' },
  },
  component: WorkspaceModelsPage,
})

function WorkspaceModelsPage() {
  const { workspaceSlug } = Route.useParams()
  const me = Route.useLoaderData()
  const membership = me.workspaces.find((item) => item.slug === workspaceSlug)
  const canManage = membership?.role === 'owner' || membership?.role === 'admin'

  return (
    <Main>
      <ModelProviderListPage
        scope='workspace'
        workspaceSlug={workspaceSlug}
        canManage={canManage}
      />
    </Main>
  )
}
