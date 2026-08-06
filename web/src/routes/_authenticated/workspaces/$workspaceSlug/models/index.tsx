import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { Main } from '@/components/layout/main'
import { meQueryOptions } from '@/features/auth/queries'
import { ModelServicePage } from '@/features/models/model-service-page'
import { modelProvidersQueryOptions } from '@/features/models/queries'
import { modelServiceSearchSchema } from '@/features/models/search-params'

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/models/'
)({
  validateSearch: modelServiceSearchSchema,
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
  const search = Route.useSearch()
  const navigate = useNavigate({ from: Route.fullPath })
  const membership = me.workspaces.find((item) => item.slug === workspaceSlug)
  const canManage = membership?.role === 'owner' || membership?.role === 'admin'

  return (
    <Main>
      <ModelServicePage
        scope='workspace'
        workspaceSlug={workspaceSlug}
        canManage={canManage}
        search={search}
        onSearchChange={(next) =>
          void navigate({ search: (current) => ({ ...current, ...next }) })
        }
      />
    </Main>
  )
}
