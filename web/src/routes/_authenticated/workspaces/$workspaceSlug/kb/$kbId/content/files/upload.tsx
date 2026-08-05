import { createFileRoute, redirect } from '@tanstack/react-router'
import { z } from 'zod'

const uploadSearchSchema = z.object({ parent: z.string().optional() })

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/kb/$kbId/content/files/upload'
)({
  validateSearch: uploadSearchSchema,
  beforeLoad: ({ params, search }) => {
    throw redirect({
      to: '/workspaces/$workspaceSlug/kb/$kbId/content/files',
      params,
      search: { folder: search.parent, upload: true },
      replace: true,
    })
  },
})
