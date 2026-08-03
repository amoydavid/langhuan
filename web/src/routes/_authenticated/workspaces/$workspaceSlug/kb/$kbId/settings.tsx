import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { meQueryOptions } from '@/features/auth/queries'
import { indexGenerationsQueryOptions } from '@/features/index-generations/queries'
import { canManageIndex } from '@/features/knowledge-bases/permissions'
import { knowledgeBaseQueryOptions } from '@/features/knowledge-bases/queries'
import { KnowledgeBaseSettings } from '@/features/knowledge-bases/settings/knowledge-base-settings'
import { updateKnowledgeBaseBasics } from '@/features/knowledge-bases/workbench/api'
import { knowledgeBaseSummaryQueryOptions } from '@/features/knowledge-bases/workbench/queries'
import type { UpdateKnowledgeBaseBasicsInput } from '@/features/knowledge-bases/workbench/types'

function SettingsPage() {
  const { t } = useTranslation()
  const { workspaceSlug, kbId } = Route.useParams()
  const queryClient = useQueryClient()
  const { data: knowledgeBase } = useQuery(
    knowledgeBaseQueryOptions(workspaceSlug, kbId)
  )
  const { data: summary } = useQuery(
    knowledgeBaseSummaryQueryOptions(workspaceSlug, kbId)
  )
  const { data: generations = [] } = useQuery(
    indexGenerationsQueryOptions(workspaceSlug, kbId)
  )
  const { data: me } = useQuery(meQueryOptions())
  const role = me?.workspaces.find((item) => item.slug === workspaceSlug)?.role
  const canManage = canManageIndex(role)
  const activeGeneration = generations.find(
    (item) => item.id === summary?.active_generation?.id
  )
  const mutation = useMutation({
    mutationFn: (input: UpdateKnowledgeBaseBasicsInput) =>
      updateKnowledgeBaseBasics(workspaceSlug, kbId, input),
    onSuccess: async (updated) => {
      queryClient.setQueryData(
        knowledgeBaseQueryOptions(workspaceSlug, kbId).queryKey,
        updated
      )
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: ['knowledge-bases', workspaceSlug],
        }),
        queryClient.invalidateQueries({
          queryKey: ['knowledge-base-summary', workspaceSlug, kbId],
        }),
      ])
      toast.success(t('routes.workspaces.kb.settings.savedToast'))
    },
  })

  if (!knowledgeBase) return null

  return (
    <KnowledgeBaseSettings
      knowledgeBase={knowledgeBase}
      activeGeneration={activeGeneration}
      canManage={canManage}
      saveBasics={(input) => mutation.mutateAsync(input)}
      copyText={(text) => navigator.clipboard.writeText(text)}
      buildIndexHref={`/workspaces/${encodeURIComponent(workspaceSlug)}/kb/${encodeURIComponent(kbId)}/indexes?create=true`}
    />
  )
}

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/kb/$kbId/settings'
)({
  loader: ({ context, params }) =>
    Promise.all([
      context.queryClient.ensureQueryData(
        knowledgeBaseQueryOptions(params.workspaceSlug, params.kbId)
      ),
      context.queryClient.ensureQueryData(
        knowledgeBaseSummaryQueryOptions(params.workspaceSlug, params.kbId)
      ),
      context.queryClient.ensureQueryData(
        indexGenerationsQueryOptions(params.workspaceSlug, params.kbId)
      ),
      context.queryClient.ensureQueryData(meQueryOptions()),
    ]),
  staticData: {
    breadcrumb: { label: 'routes.workspaces.kb.settings.breadcrumb' },
  },
  component: SettingsPage,
})
