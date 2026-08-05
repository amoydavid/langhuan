import { createFileRoute } from '@tanstack/react-router'
export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/kb/$kbId/content/files/'
)({
  component: () => null,
})
