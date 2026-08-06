import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, notFound } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Main } from '@/components/layout/main'
import { meQueryOptions } from '@/features/auth/queries'
import { selectableModelsQueryOptions } from '@/features/models/queries'
import { updateWorkspaceSearchSettings } from '@/features/search-settings/api'
import { workspaceSearchSettingsQueryOptions } from '@/features/search-settings/queries'
import { SearchSettingsForm } from '@/features/search-settings/search-settings-form'

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/search-settings'
)({
  loader: async ({ context, params }) => {
    const me = await context.queryClient.ensureQueryData(meQueryOptions())
    const membership = me.workspaces.find(
      (item) => item.slug === params.workspaceSlug
    )
    if (
      !membership ||
      (membership.role !== 'owner' && membership.role !== 'admin')
    ) {
      throw notFound()
    }
    await Promise.all([
      context.queryClient.ensureQueryData(
        workspaceSearchSettingsQueryOptions(params.workspaceSlug)
      ),
      context.queryClient.ensureQueryData(
        selectableModelsQueryOptions(params.workspaceSlug, 'rerank')
      ),
    ])
    return { me, membership }
  },
  staticData: {
    breadcrumb: { label: 'routes.workspaces.searchSettings.breadcrumb' },
  },
  component: SearchSettingsPage,
})

function SearchSettingsPage() {
  const { t } = useTranslation()
  const { workspaceSlug } = Route.useParams()
  const { membership } = Route.useLoaderData()
  const queryClient = useQueryClient()
  const { data: settings } = useQuery(
    workspaceSearchSettingsQueryOptions(workspaceSlug)
  )
  const { data: models = [] } = useQuery(
    selectableModelsQueryOptions(workspaceSlug, 'rerank')
  )
  const mutation = useMutation({
    mutationFn: (input: Parameters<typeof updateWorkspaceSearchSettings>[1]) =>
      updateWorkspaceSearchSettings(workspaceSlug, input),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ['workspace-search-settings', workspaceSlug],
      })
      toast.success(t('searchSettings.form.saved'))
    },
  })

  if (membership.role !== 'owner' && membership.role !== 'admin') return null
  if (!settings) return null

  return (
    <Main>
      <div className='space-y-6'>
        <div>
          <p className='page-eyebrow'>
            {t('routes.workspaces.searchSettings.eyebrow')}
          </p>
          <h1 className='font-semibold text-2xl tracking-tight'>
            {t('routes.workspaces.searchSettings.title')}
          </h1>
          <p className='mt-2 text-muted-foreground'>
            {t('routes.workspaces.searchSettings.description')}
          </p>
        </div>
        <div className='rounded-xl border bg-card p-6'>
          <SearchSettingsForm
            settings={settings}
            models={models}
            save={async (values) => {
              await mutation.mutateAsync({
                rerank: values.rerank_enabled
                  ? {
                      enabled: true,
                      model_id: values.rerank_model_id,
                      candidate_top_k: values.candidate_top_k,
                      failure_mode: values.failure_mode,
                    }
                  : { enabled: false },
              })
            }}
          />
        </div>
      </div>
    </Main>
  )
}
