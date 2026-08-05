import { createFileRoute } from '@tanstack/react-router'
import { z } from 'zod'
import { FileTreeWorkspace } from '@/features/content/file-tree/file-tree-workspace'
import { fileTreeQueryOptions } from '@/features/content/file-tree/queries'

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/kb/$kbId/content/files'
)({
  validateSearch: z.object({
    folder: z.string().optional(),
    upload: z.boolean().optional(),
  }),
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(
      fileTreeQueryOptions(params.workspaceSlug, params.kbId)
    ),
  staticData: {
    breadcrumb: {
      label: 'routes.workspaces.kb.content.files.breadcrumb',
    },
    fullHeight: true,
  },
  component: FileTreeWorkspace,
})
