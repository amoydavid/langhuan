import { createFileRoute, redirect } from '@tanstack/react-router'
import { z } from 'zod'

const searchSchema = z.object({ parent: z.string().optional() })

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/kb/$kbId/documents/new'
)({
  validateSearch: searchSchema,
  loaderDeps: ({ search }) => search,
  loader: ({ params, deps }) =>
    redirect({
      href: `/workspaces/${encodeURIComponent(params.workspaceSlug)}/kb/${encodeURIComponent(params.kbId)}/content/files/upload${deps.parent ? `?parent=${encodeURIComponent(deps.parent)}` : ''}`,
      replace: true,
    }),
  staticData: {
    breadcrumb: {
      label: 'routes.workspaces.kb.documents.new.breadcrumb',
    },
  },
  component: () => null,
})
