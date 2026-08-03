import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/kb/$kbId/content/'
)({
  loader: ({ params }) =>
    redirect({
      to: '/workspaces/$workspaceSlug/kb/$kbId/content/all',
      params,
      replace: true,
    }),
})
