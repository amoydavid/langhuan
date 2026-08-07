import { createFileRoute, redirect } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { Main } from '@/components/layout/main'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { workspaceEntry } from '@/features/auth/navigation'
import { meQueryOptions } from '@/features/auth/queries'
import { WorkspaceForm } from '@/features/workspaces/components/workspace-form'

export const Route = createFileRoute('/_authenticated/workspaces/new')({
  loader: async ({ context }) => {
    const me = await context.queryClient.ensureQueryData(meQueryOptions())
    // 单租户模式且已有唯一 workspace：创建入口对 platform_admin 已隐藏，
    // 直接访问该 URL 时重定向到现有 workspace。
    if (me.single_tenant && me.workspaces.length > 0) {
      throw redirect({
        href: workspaceEntry(me.workspaces[0].slug),
        replace: true,
      })
    }
    return me
  },
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
