import { createFileRoute, notFound, Outlet } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import type { RouteBreadcrumb } from '@/components/layout/app-breadcrumbs'
import { Main } from '@/components/layout/main'
import { workspaceQueryOptions } from '@/features/workspaces/queries'
import { parseApiError } from '@/lib/api/error'

type WorkspaceLoaderData = { workspaceName: string }

function workspaceName(loaderData: unknown) {
  if (
    typeof loaderData === 'object' &&
    loaderData !== null &&
    'workspaceName' in loaderData &&
    typeof loaderData.workspaceName === 'string'
  ) {
    return loaderData.workspaceName
  }
  return undefined
}

const breadcrumb: RouteBreadcrumb = {
  label: 'routes.workspaces.workspace.breadcrumb',
  resolve: workspaceName,
}

function WorkspaceNotFound() {
  const { t } = useTranslation()
  return (
    <Main>
      <div className='mx-auto flex min-h-80 max-w-lg flex-col items-center justify-center text-center'>
        <p className='font-medium text-primary text-sm'>404</p>
        <h1 className='mt-2 font-semibold text-2xl'>
          {t('routes.workspaces.notFound.title')}
        </h1>
        <p className='mt-2 text-muted-foreground'>
          {t('routes.workspaces.notFound.description')}
        </p>
      </div>
    </Main>
  )
}

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug'
)({
  loader: async ({ context, params }): Promise<WorkspaceLoaderData> => {
    try {
      const workspace = await context.queryClient.ensureQueryData(
        workspaceQueryOptions(params.workspaceSlug)
      )
      return { workspaceName: workspace.name }
    } catch (error) {
      if (parseApiError(error).status === 404) throw notFound()
      throw error
    }
  },
  staticData: { breadcrumb },
  notFoundComponent: WorkspaceNotFound,
  component: Outlet,
})
