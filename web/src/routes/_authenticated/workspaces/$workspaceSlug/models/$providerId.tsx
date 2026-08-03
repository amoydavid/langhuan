import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { Main } from '@/components/layout/main'
import { meQueryOptions } from '@/features/auth/queries'
import { ModelProviderDetailPage } from '@/features/models/model-provider-detail-page'
import {
  modelProviderQueryOptions,
  modelsQueryOptions,
} from '@/features/models/queries'

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/models/$providerId'
)({
  loader: async ({ context, params }) => {
    const [me, provider] = await Promise.all([
      context.queryClient.ensureQueryData(meQueryOptions()),
      context.queryClient.ensureQueryData(
        modelProviderQueryOptions(
          'workspace',
          params.providerId,
          params.workspaceSlug
        )
      ),
      context.queryClient.ensureQueryData(
        modelsQueryOptions('workspace', params.providerId, params.workspaceSlug)
      ),
    ])
    return { me, providerName: provider.display_name }
  },
  staticData: {
    breadcrumb: { label: 'routes.workspaces.models.detail.breadcrumb' },
  },
  component: WorkspaceModelProviderPage,
})

function WorkspaceModelProviderPage() {
  const { workspaceSlug, providerId } = Route.useParams()
  const { me } = Route.useLoaderData()
  const navigate = useNavigate()
  const membership = me.workspaces.find((item) => item.slug === workspaceSlug)
  const canManage = membership?.role === 'owner' || membership?.role === 'admin'

  return (
    <Main>
      <ModelProviderDetailPage
        scope='workspace'
        workspaceSlug={workspaceSlug}
        providerId={providerId}
        canManage={canManage}
        onProviderDeleted={() =>
          void navigate({
            to: '/workspaces/$workspaceSlug/models',
            params: { workspaceSlug },
          })
        }
      />
    </Main>
  )
}
