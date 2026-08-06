import { createFileRoute } from '@tanstack/react-router'
import { Main } from '@/components/layout/main'
import { ModelProviderListPage } from '@/features/models/model-provider-list-page'
import { modelProvidersQueryOptions } from '@/features/models/queries'
import { modelServiceSearchSchema } from '@/features/models/search-params'

export const Route = createFileRoute('/_authenticated/admin/models/')({
  validateSearch: modelServiceSearchSchema,
  loader: ({ context }) =>
    context.queryClient.ensureQueryData(modelProvidersQueryOptions('platform')),
  staticData: {
    breadcrumb: { label: 'routes.admin.models.breadcrumb' },
  },
  component: () => (
    <Main>
      <ModelProviderListPage scope='platform' canManage />
    </Main>
  ),
})
