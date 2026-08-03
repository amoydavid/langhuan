import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { Main } from '@/components/layout/main'
import { ModelProviderDetailPage } from '@/features/models/model-provider-detail-page'
import {
  modelProviderQueryOptions,
  modelsQueryOptions,
} from '@/features/models/queries'

export const Route = createFileRoute(
  '/_authenticated/admin/models/$providerId'
)({
  loader: async ({ context, params }) => {
    const provider = await context.queryClient.ensureQueryData(
      modelProviderQueryOptions('platform', params.providerId)
    )
    await context.queryClient.ensureQueryData(
      modelsQueryOptions('platform', params.providerId)
    )
    return { providerName: provider.display_name }
  },
  staticData: {
    breadcrumb: { label: 'routes.admin.models.detail.breadcrumb' },
  },
  component: PlatformModelProviderPage,
})

function PlatformModelProviderPage() {
  const { providerId } = Route.useParams()
  const navigate = useNavigate()
  return (
    <Main>
      <ModelProviderDetailPage
        scope='platform'
        providerId={providerId}
        canManage
        onProviderDeleted={() => void navigate({ to: '/admin/models' })}
      />
    </Main>
  )
}
