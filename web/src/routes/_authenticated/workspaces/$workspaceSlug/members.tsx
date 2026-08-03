import { createFileRoute } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { Main } from '@/components/layout/main'
import { meQueryOptions } from '@/features/auth/queries'
import { MemberList } from '@/features/members/member-list'
import { membersQueryOptions } from '@/features/members/queries'

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/members'
)({
  loader: async ({ context, params }) => {
    const [me] = await Promise.all([
      context.queryClient.ensureQueryData(meQueryOptions()),
      context.queryClient.ensureQueryData(
        membersQueryOptions(params.workspaceSlug)
      ),
    ])
    return me
  },
  staticData: {
    breadcrumb: { label: 'routes.workspaces.members.breadcrumb' },
  },
  component: MembersPage,
})

function MembersPage() {
  const { t } = useTranslation()
  const me = Route.useLoaderData()
  const { workspaceSlug } = Route.useParams()
  const membership = me.workspaces.find((item) => item.slug === workspaceSlug)
  if (!membership) return null

  return (
    <Main>
      <div className='space-y-6'>
        <div>
          <p className='page-eyebrow'>
            {t('routes.workspaces.members.eyebrow')}
          </p>
          <h1 className='font-semibold text-2xl tracking-tight'>
            {t('routes.workspaces.members.title')}
          </h1>
          <p className='mt-2 text-muted-foreground'>
            {t('routes.workspaces.members.description')}
          </p>
        </div>
        <MemberList
          workspaceSlug={workspaceSlug}
          actorRole={membership.role}
          isPlatformAdmin={me.user.is_platform_admin}
        />
      </div>
    </Main>
  )
}
