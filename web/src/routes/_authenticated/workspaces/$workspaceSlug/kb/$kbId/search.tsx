import { useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { AlertCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { retrievalConfigResponseSchema } from '@/features/knowledge-bases/schemas'
import { knowledgeBaseSummaryQueryOptions } from '@/features/knowledge-bases/workbench/queries'
import {
  RetrievalTest,
  retrievalSearchSchema,
} from '@/features/retrieval/retrieval-test'

const fallbackDefaults = {
  fts_config: 'zhparser',
  vector_top_k: 20,
  keyword_top_k: 20,
  final_top_k: 8,
  rrf_k: 60,
}

function SearchPage() {
  const { t } = useTranslation()
  const { workspaceSlug, kbId } = Route.useParams()
  // URL 深链仅用于初始化表单值；检索本身由组件内的 AJAX 驱动，不再改变 URL。
  const search = Route.useSearch()
  const { data: summary } = useQuery(
    knowledgeBaseSummaryQueryOptions(workspaceSlug, kbId)
  )
  const defaults = retrievalConfigResponseSchema.safeParse(
    summary?.active_generation?.retrieval_config
  )
  const retrievalDefaults = defaults.success ? defaults.data : fallbackDefaults

  if (!summary?.active_generation) {
    return (
      <Alert>
        <AlertCircle />
        <AlertTitle>{t('routes.workspaces.kb.search.noIndexTitle')}</AlertTitle>
        <AlertDescription>
          {t('routes.workspaces.kb.search.noIndexDescription')}
        </AlertDescription>
      </Alert>
    )
  }

  return (
    <RetrievalTest
      workspaceSlug={workspaceSlug}
      kbId={kbId}
      search={search}
      defaults={retrievalDefaults}
      activeGenerationLabel={summary.active_generation.display_label}
    />
  )
}

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/kb/$kbId/search'
)({
  validateSearch: retrievalSearchSchema,
  loader: async ({ context, params }) => {
    // 仅预取知识库摘要；检索结果由组件 AJAX 按需获取，避免 URL 驱动的页面跳转。
    await context.queryClient.ensureQueryData(
      knowledgeBaseSummaryQueryOptions(params.workspaceSlug, params.kbId)
    )
  },
  staticData: {
    breadcrumb: { label: 'routes.workspaces.kb.search.breadcrumb' },
  },
  component: SearchPage,
})
