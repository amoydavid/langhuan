import { createFileRoute, Link } from '@tanstack/react-router'
import { Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Main } from '@/components/layout/main'
import { Button } from '@/components/ui/button'
import { meQueryOptions } from '@/features/auth/queries'
import { SourceConnectionList } from '@/features/integrations/components/source-connection-list'
import { sourceConnectionsQueryOptions } from '@/features/integrations/queries'

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/integrations/'
)({
  loader: async ({ context, params }) => {
    const [me] = await Promise.all([
      context.queryClient.ensureQueryData(meQueryOptions()),
      context.queryClient.ensureQueryData(
        sourceConnectionsQueryOptions(params.workspaceSlug)
      ),
    ])
    return { me }
  },
  staticData: {
    breadcrumb: { label: 'routes.workspaces.integrations.breadcrumb' },
  },
  component: IntegrationsPage,
})

function IntegrationsPage() {
  const { t } = useTranslation()
  const { workspaceSlug } = Route.useParams()
  const { me } = Route.useLoaderData()
  const membership = me.workspaces.find((item) => item.slug === workspaceSlug)
  const canManage = membership?.role === 'owner' || membership?.role === 'admin'

  return (
    <Main>
      <div className='space-y-6'>
        <div className='flex flex-col justify-between gap-4 sm:flex-row sm:items-end'>
          <div>
            <p className='page-eyebrow'>{t('integrations.list.eyebrow')}</p>
            <h1 className='font-semibold text-2xl tracking-tight'>
              {t('integrations.list.title')}
            </h1>
            <p className='mt-2 max-w-2xl text-muted-foreground'>
              {t('integrations.list.subtitle')}
            </p>
          </div>
          {canManage && (
            <Button asChild>
              <Link
                to='/workspaces/$workspaceSlug/integrations/new'
                params={{ workspaceSlug }}
              >
                <Plus />
                {t('integrations.list.createButton')}
              </Link>
            </Button>
          )}
        </div>
        <SourceConnectionList
          workspaceSlug={workspaceSlug}
          workspaceRole={membership?.role ?? 'member'}
        />
      </div>
    </Main>
  )
}
