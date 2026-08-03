import { createFileRoute } from '@tanstack/react-router'
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
import { WorkspaceForm } from '@/features/workspaces/components/workspace-form'

export const Route = createFileRoute('/_authenticated/workspaces/new')({
  loader: ({ context }) =>
    context.queryClient.ensureQueryData(meQueryOptions()),
  staticData: {
    breadcrumb: { label: 'routes.workspaces.new.breadcrumb' },
  },
  component: NewWorkspacePage,
})

function NewWorkspacePage() {
  const { t } = useTranslation()
  const me = Route.useLoaderData()
  return (
    <Main>
      <div className='mx-auto max-w-2xl space-y-6'>
        <div>
          <p className='page-eyebrow'>{t('routes.workspaces.new.eyebrow')}</p>
          <h1 className='font-semibold text-2xl tracking-tight'>
            {t('routes.workspaces.new.title')}
          </h1>
          <p className='mt-2 text-muted-foreground'>
            {t('routes.workspaces.new.description')}
          </p>
        </div>
        <Card>
          <CardHeader>
            <CardTitle>{t('routes.workspaces.new.cardTitle')}</CardTitle>
            <CardDescription>
              {t('routes.workspaces.new.cardDescription')}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <WorkspaceForm isPlatformAdmin={me.user.is_platform_admin} />
          </CardContent>
        </Card>
      </div>
    </Main>
  )
}
