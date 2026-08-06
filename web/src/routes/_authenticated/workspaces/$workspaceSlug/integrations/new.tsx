import { createFileRoute, notFound } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { Main } from '@/components/layout/main'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { meQueryOptions } from '@/features/auth/queries'
import { SourceConnectionForm } from '@/features/integrations/components/source-connection-form'

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/integrations/new'
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
    return me
  },
  staticData: {
    breadcrumb: { label: 'routes.workspaces.integrations.new.breadcrumb' },
  },
  component: NewIntegrationPage,
})

function NewIntegrationPage() {
  const { t } = useTranslation()
  const { workspaceSlug } = Route.useParams()
  return (
    <Main>
      <div className='mx-auto max-w-3xl space-y-6'>
        <div>
          <p className='page-eyebrow'>
            {t('routes.workspaces.integrations.new.eyebrow')}
          </p>
          <h1 className='font-semibold text-2xl tracking-tight'>
            {t('routes.workspaces.integrations.new.title')}
          </h1>
          <p className='mt-2 text-muted-foreground'>
            {t('routes.workspaces.integrations.new.description')}
          </p>
        </div>
        <Card>
          <CardHeader>
            <CardTitle>
              {t('routes.workspaces.integrations.new.cardTitle')}
            </CardTitle>
            <CardDescription>
              {t('routes.workspaces.integrations.new.cardDescription')}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <SourceConnectionForm workspaceSlug={workspaceSlug} />
          </CardContent>
        </Card>
      </div>
    </Main>
  )
}
