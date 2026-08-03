import { createFileRoute, notFound } from '@tanstack/react-router'
import { APIKeyCreatePage } from '@/features/api-keys/api-key-create-page'
import { knowledgeBasesQueryOptions } from '@/features/knowledge-bases/queries'
import { parseApiError } from '@/lib/api/error'

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/api-keys/new'
)({
  loader: async ({ context, params }) => {
    try {
      const knowledgeBases = await context.queryClient.ensureQueryData(
        knowledgeBasesQueryOptions(params.workspaceSlug)
      )
      return { knowledgeBases }
    } catch (error) {
      if (parseApiError(error).status === 404) throw notFound()
      throw error
    }
  },
  staticData: {
    breadcrumb: { label: 'routes.workspaces.apiKeys.new.breadcrumb' },
  },
  component: NewAPIKeyRoute,
})

function NewAPIKeyRoute() {
  const { workspaceSlug } = Route.useParams()
  const { knowledgeBases } = Route.useLoaderData()
  return (
    <APIKeyCreatePage
      key={workspaceSlug}
      knowledgeBases={knowledgeBases.map((kb) => ({
        id: kb.id,
        name: kb.name,
      }))}
    />
  )
}
