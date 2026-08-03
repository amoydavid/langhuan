import { createFileRoute, notFound } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { Main } from '@/components/layout/main'
import { meQueryOptions } from '@/features/auth/queries'
import { InvitationForm } from '@/features/invitations/components/invitation-form'
import { InvitationList } from '@/features/invitations/invitation-list'
import { invitationsQueryOptions } from '@/features/invitations/queries'

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/invitations'
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
    await context.queryClient.ensureQueryData(
      invitationsQueryOptions(params.workspaceSlug)
    )
    return { me, membership }
  },
  staticData: {
    breadcrumb: { label: 'routes.workspaces.invitations.breadcrumb' },
  },
  component: InvitationsPage,
})

function InvitationsPage() {
  const { t } = useTranslation()
  const { me, membership } = Route.useLoaderData()
  const { workspaceSlug } = Route.useParams()
  if (membership.role !== 'owner' && membership.role !== 'admin') return null
  return (
    <Main>
      <div className='space-y-6'>
        <div>
          <p className='page-eyebrow'>
            {t('routes.workspaces.invitations.eyebrow')}
          </p>
          <h1 className='font-semibold text-2xl tracking-tight'>
            {t('routes.workspaces.invitations.title')}
          </h1>
          <p className='mt-2 text-muted-foreground'>
            {t('routes.workspaces.invitations.description')}
          </p>
        </div>
        <div className='rounded-xl border bg-card p-6'>
          <InvitationForm
            workspaceSlug={workspaceSlug}
            actorRole={membership.role}
          />
        </div>
        <InvitationList
          workspaceSlug={workspaceSlug}
          actorRole={membership.role}
          actorUserId={me.user.id}
          isPlatformAdmin={me.user.is_platform_admin}
        />
      </div>
    </Main>
  )
}
