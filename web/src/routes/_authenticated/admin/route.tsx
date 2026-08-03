import { createFileRoute, notFound, Outlet } from '@tanstack/react-router'
import { meQueryOptions } from '@/features/auth/queries'

export const Route = createFileRoute('/_authenticated/admin')({
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.ensureQueryData(meQueryOptions())
    if (!me.user.is_platform_admin) throw notFound()
  },
  staticData: { breadcrumb: { label: 'routes.admin.breadcrumb' } },
  component: Outlet,
})
