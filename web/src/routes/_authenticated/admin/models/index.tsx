import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { Main } from '@/components/layout/main'
import { ModelServicePage } from '@/features/models/model-service-page'
import { modelProvidersQueryOptions } from '@/features/models/queries'
import { modelServiceSearchSchema } from '@/features/models/search-params'

export const Route = createFileRoute('/_authenticated/admin/models/')({
  validateSearch: modelServiceSearchSchema,
  loader: ({ context }) =>
    context.queryClient.ensureQueryData(modelProvidersQueryOptions('platform')),
  staticData: {
    breadcrumb: { label: 'routes.admin.models.breadcrumb' },
  },
  component: PlatformModelsPage,
})

function PlatformModelsPage() {
  const search = Route.useSearch()
  const navigate = useNavigate({ from: Route.fullPath })
  return (
    <Main>
      <ModelServicePage
        scope='platform'
        canManage
        search={search}
        onSearchChange={(next) =>
          void navigate({ search: (current) => ({ ...current, ...next }) })
        }
      />
    </Main>
  )
}
