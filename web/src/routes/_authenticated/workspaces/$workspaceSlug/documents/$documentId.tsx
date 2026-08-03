import { createFileRoute, redirect } from '@tanstack/react-router'
import { z } from 'zod'
import type { RouteBreadcrumb } from '@/components/layout/app-breadcrumbs'
import { canonicalDocumentHref } from '@/features/content/routing'
import { documentQueryOptions } from '@/features/documents/queries'

const searchSchema = z.object({ job: z.string().optional() })

function documentTitle(loaderData: unknown) {
  if (
    typeof loaderData === 'object' &&
    loaderData !== null &&
    'title' in loaderData &&
    typeof loaderData.title === 'string'
  ) {
    return loaderData.title
  }
  return undefined
}

const breadcrumb: RouteBreadcrumb = {
  label: 'routes.workspaces.documents.detail.breadcrumb',
  resolve: documentTitle,
}

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/documents/$documentId'
)({
  validateSearch: searchSchema,
  loaderDeps: ({ search }) => search,
  loader: async ({ context, params, deps }) => {
    const item = await context.queryClient.ensureQueryData(
      documentQueryOptions(params.workspaceSlug, params.documentId)
    )
    const href = canonicalDocumentHref(params.workspaceSlug, item)
    const job = deps.job
    throw redirect({
      href: job ? `${href}?job=${encodeURIComponent(String(job))}` : href,
      replace: true,
    })
  },
  staticData: { breadcrumb },
  component: () => null,
})
