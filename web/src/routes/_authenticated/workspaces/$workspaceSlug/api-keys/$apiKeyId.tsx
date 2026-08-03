import { createFileRoute, notFound } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { APIKeyDetailPage } from '@/features/api-keys/api-key-detail-page'
import { apiKeyQueryOptions } from '@/features/api-keys/queries'
import { parseApiError } from '@/lib/api/error'

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/api-keys/$apiKeyId'
)({
  loader: async ({ context, params }) => {
    try {
      const data = await context.queryClient.ensureQueryData(
        apiKeyQueryOptions(params.workspaceSlug, params.apiKeyId)
      )
      return data
    } catch (error) {
      const apiError = parseApiError(error)
      // 404 或 403（无权访问该 key）统一显示「不存在或无权访问」。
      if (apiError.status === 404 || apiError.status === 403) throw notFound()
      throw error
    }
  },
  staticData: {
    breadcrumb: {
      label: 'routes.workspaces.apiKeys.detail.breadcrumb',
    },
  },
  notFoundComponent: APIKeyNotFound,
  component: APIKeyDetailRoute,
})

function APIKeyNotFound() {
  const { t } = useTranslation()
  return (
    <div className='mx-auto flex min-h-80 max-w-lg flex-col items-center justify-center text-center'>
      <p className='font-medium text-primary text-sm'>404</p>
      <h1 className='mt-2 font-semibold text-2xl'>
        {t('routes.workspaces.apiKeys.detail.notFoundTitle')}
      </h1>
      <p className='mt-2 text-muted-foreground'>
        {t('routes.workspaces.apiKeys.detail.notFoundDescription')}
      </p>
    </div>
  )
}

function APIKeyDetailRoute() {
  const data = Route.useLoaderData()
  const { workspaceSlug, apiKeyId } = Route.useParams()
  return <APIKeyDetailPage key={`${workspaceSlug}:${apiKeyId}`} data={data} />
}
