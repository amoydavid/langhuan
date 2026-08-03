import { createFileRoute, Outlet } from '@tanstack/react-router'
import type { RouteBreadcrumb } from '@/components/layout/app-breadcrumbs'
import { Main } from '@/components/layout/main'
import { meQueryOptions } from '@/features/auth/queries'
import { ForbiddenError } from '@/features/errors/forbidden'

type APIKeysLoaderData = { canManage: boolean }

const breadcrumb: RouteBreadcrumb = { label: 'API Key' }

// 直接命中 api-keys 路由（含子路由）时统一鉴权：
// owner/admin 可访问；member 渲染 403（ForbiddenError）而非静默跳转。
export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/api-keys'
)({
  loader: async ({ context, params }): Promise<APIKeysLoaderData> => {
    const me = await context.queryClient.ensureQueryData(meQueryOptions())
    const membership = me.workspaces.find(
      (item) => item.slug === params.workspaceSlug
    )
    const canManage =
      membership?.role === 'owner' || membership?.role === 'admin'
    return { canManage }
  },
  staticData: { breadcrumb },
  component: APIKeysLayout,
})

function APIKeysLayout() {
  const { canManage } = Route.useLoaderData()
  if (!canManage) {
    return <ForbiddenError />
  }
  return (
    <Main>
      <Outlet />
    </Main>
  )
}
