import { createFileRoute } from '@tanstack/react-router'
import { APIKeyListPage } from '@/features/api-keys/api-key-list-page'
import { apiKeysQueryOptions } from '@/features/api-keys/queries'

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/api-keys/'
)({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(
      apiKeysQueryOptions(params.workspaceSlug)
    ),
  staticData: { breadcrumb: { label: 'routes.workspaces.apiKeys.breadcrumb' } },
  component: () => <APIKeyListPage />,
})
