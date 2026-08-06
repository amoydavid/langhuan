import { createFileRoute } from '@tanstack/react-router'
import { Main } from '@/components/layout/main'
import { meQueryOptions } from '@/features/auth/queries'
import { MemberList } from '@/features/members/member-list'
import { MemberPageHeader } from '@/features/members/member-page-header'
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
  const me = Route.useLoaderData()
  const { workspaceSlug } = Route.useParams()
  const membership = me.workspaces.find((item) => item.slug === workspaceSlug)
  if (!membership) return null

  return (
    <Main>
      <div className='space-y-6'>
        <MemberPageHeader
          workspaceSlug={workspaceSlug}
          actorRole={membership.role}
        />
        <MemberList
          workspaceSlug={workspaceSlug}
          actorRole={membership.role}
          isPlatformAdmin={me.user.is_platform_admin}
        />
      </div>
    </Main>
  )
}
