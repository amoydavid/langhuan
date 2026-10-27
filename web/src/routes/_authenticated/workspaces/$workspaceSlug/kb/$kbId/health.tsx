import { useQuery } from '@tanstack/react-query'
import { createFileRoute, notFound } from '@tanstack/react-router'
import { meQueryOptions } from '@/features/auth/queries'
import {
  InsightsMemberNotice,
  InsightsPage,
} from '@/features/insights/components/insights-page'
import {
  insightsActivityQueryOptions,
  insightsOverviewQueryOptions,
  sleepingDocumentsQueryOptions,
  zeroRecallQueriesQueryOptions,
} from '@/features/insights/queries'
import { insightsSearchSchema } from '@/features/insights/schemas'
import {
  DEFAULT_INSIGHTS_WINDOW_DAYS,
  type InsightsWindowDays,
} from '@/features/insights/types'
import { parseApiError } from '@/lib/api/error'

function HealthPage() {
  const { workspaceSlug, kbId } = Route.useParams()
  const search = Route.useSearch()
  const navigate = Route.useNavigate()
  const { data: me } = useQuery(meQueryOptions())
  const role = me?.workspaces.find((item) => item.slug === workspaceSlug)?.role
  // insights 端点为 Session owner/admin（与 source-connections 同级）：
  // member 直接展示说明态，不发起任何数据请求。
  if (role !== 'admin' && role !== 'owner') return <InsightsMemberNotice />

  const windowDays: InsightsWindowDays =
    search.window ?? DEFAULT_INSIGHTS_WINDOW_DAYS
  return (
    <InsightsPage
      workspaceSlug={workspaceSlug}
      kbId={kbId}
      windowDays={windowDays}
      onWindowChange={(next) =>
        void navigate({
          search: (prev) => ({ ...prev, window: next }),
          replace: true,
        })
      }
    />
  )
}

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/kb/$kbId/health'
)({
  validateSearch: insightsSearchSchema,
  loader: async ({ context, params }) => {
    const me = await context.queryClient.ensureQueryData(meQueryOptions())
    const role = me.workspaces.find(
      (item) => item.slug === params.workspaceSlug
    )?.role
    if (role !== 'admin' && role !== 'owner') return
    try {
      await Promise.all([
        context.queryClient.ensureQueryData(
          insightsOverviewQueryOptions(
            params.workspaceSlug,
            params.kbId,
            DEFAULT_INSIGHTS_WINDOW_DAYS
          )
        ),
        context.queryClient.ensureQueryData(
          zeroRecallQueriesQueryOptions(
            params.workspaceSlug,
            params.kbId,
            DEFAULT_INSIGHTS_WINDOW_DAYS
          )
        ),
        context.queryClient.ensureQueryData(
          sleepingDocumentsQueryOptions(params.workspaceSlug, params.kbId)
        ),
        context.queryClient.ensureQueryData(
          insightsActivityQueryOptions(params.workspaceSlug, params.kbId)
        ),
      ])
    } catch (error) {
      const apiError = parseApiError(error)
      // 角色缓存过期（降权后 me 未刷新）：交给组件按 403 展示权限说明态。
      if (apiError.status === 403) return
      if (apiError.status === 404) throw notFound()
      throw error
    }
  },
  staticData: {
    breadcrumb: { label: 'insights.breadcrumb' },
  },
  component: HealthPage,
})
