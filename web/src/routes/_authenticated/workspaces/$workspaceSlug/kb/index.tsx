import { createFileRoute } from '@tanstack/react-router'
import { Main } from '@/components/layout/main'
import { KnowledgeBaseList } from '@/features/knowledge-bases/knowledge-base-list'
import { knowledgeBasesQueryOptions } from '@/features/knowledge-bases/queries'

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/kb/'
)({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(
      knowledgeBasesQueryOptions(params.workspaceSlug)
    ),
  staticData: {
    breadcrumb: { label: 'routes.workspaces.kb.breadcrumb' },
  },
  component: () => (
    <Main>
      <KnowledgeBaseList />
    </Main>
  ),
})
