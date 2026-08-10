import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { Plus, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { meQueryOptions } from '@/features/auth/queries'
import {
  activateIndexGeneration,
  createIndexGeneration,
  reindexKnowledgeBase,
} from '@/features/index-generations/api'
import { invalidateGenerationExperience } from '@/features/index-generations/cache'
import { GenerationForm } from '@/features/index-generations/generation-form'
import { GenerationList } from '@/features/index-generations/generation-list'
import { IndexWriteForbidden } from '@/features/index-generations/index-write-forbidden'
import { indexGenerationsQueryOptions } from '@/features/index-generations/queries'
import type {
  CreateIndexGenerationInput,
  IndexGeneration,
} from '@/features/index-generations/types'
import { canManageIndex } from '@/features/knowledge-bases/permissions'
import { knowledgeBaseSummaryQueryOptions } from '@/features/knowledge-bases/workbench/queries'
import { selectableModelsQueryOptions } from '@/features/models/queries'

const indexesSearchSchema = z.object({
  create: z
    .union([z.literal(true), z.literal('true')])
    .optional()
    .transform((value) => (value ? true : undefined)),
})

function IndexesPage() {
  const { t } = useTranslation()
  const { workspaceSlug, kbId } = Route.useParams()
  const search = Route.useSearch()
  const navigate = Route.useNavigate()
  const queryClient = useQueryClient()
  const { data: generations = [] } = useQuery(
    indexGenerationsQueryOptions(workspaceSlug, kbId)
  )
  const { data: summary } = useQuery(
    knowledgeBaseSummaryQueryOptions(workspaceSlug, kbId)
  )
  const { data: models = [] } = useQuery(
    selectableModelsQueryOptions(workspaceSlug)
  )
  const { data: me } = useQuery(meQueryOptions())
  const role = me?.workspaces.find((item) => item.slug === workspaceSlug)?.role
  const canManage = canManageIndex(role)
  const activeGeneration = generations.find(
    (item) => item.id === summary?.active_generation?.id
  )

  const createMutation = useMutation({
    mutationFn: (input: CreateIndexGenerationInput) =>
      createIndexGeneration(workspaceSlug, kbId, input),
    onSuccess: async () => {
      await invalidateGenerationExperience(queryClient, workspaceSlug, kbId)
      toast.success(t('routes.workspaces.kb.indexes.buildStartedToast'))
      await navigate({ search: {}, replace: true })
    },
  })
  const activationMutation = useMutation({
    mutationFn: ({
      generation,
      archiveManualEdits,
    }: {
      generation: IndexGeneration
      archiveManualEdits: boolean
    }) =>
      activateIndexGeneration(workspaceSlug, kbId, generation.id, {
        archive_manual_edits: archiveManualEdits,
      }),
    onSuccess: async () => {
      await invalidateGenerationExperience(queryClient, workspaceSlug, kbId)
      toast.success(t('routes.workspaces.kb.indexes.activatedToast'))
    },
  })
  const reindexMutation = useMutation({
    mutationFn: () => reindexKnowledgeBase(workspaceSlug, kbId),
    onSuccess: async () => {
      await invalidateGenerationExperience(queryClient, workspaceSlug, kbId)
      toast.success(t('routes.workspaces.kb.indexes.reindexStartedToast'))
    },
  })

  if (search.create && !canManage) {
    return (
      <IndexWriteForbidden
        onBack={() => void navigate({ search: {}, replace: true })}
      />
    )
  }

  return (
    <div className='space-y-5'>
      <div className='flex flex-col justify-between gap-3 sm:flex-row sm:items-start'>
        <div>
          <h2 className='font-semibold text-xl'>
            {t('routes.workspaces.kb.indexes.title')}
          </h2>
          <p className='mt-1 text-muted-foreground text-sm'>
            {t('routes.workspaces.kb.indexes.description')}
          </p>
        </div>
        {canManage && !search.create && (
          <div className='flex gap-2'>
            <Button
              variant='outline'
              onClick={() => void reindexMutation.mutateAsync()}
              disabled={reindexMutation.isPending}
            >
              <RefreshCw />
              {t('routes.workspaces.kb.indexes.reindexButton')}
            </Button>
            <Button onClick={() => void navigate({ search: { create: true } })}>
              <Plus />
              {t('routes.workspaces.kb.indexes.buildButton')}
            </Button>
          </div>
        )}
      </div>

      {search.create && canManage && activeGeneration && (
        <Card>
          <CardHeader>
            <CardTitle>
              {t('routes.workspaces.kb.indexes.candidateTitle')}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <GenerationForm
              models={models.map((model) => ({
                id: model.id,
                displayName: model.display_name,
                dimensions: model.dimensions,
              }))}
              baseGeneration={activeGeneration}
              createGeneration={(input) => createMutation.mutateAsync(input)}
              onCancel={() => void navigate({ search: {}, replace: true })}
            />
          </CardContent>
        </Card>
      )}

      <GenerationList
        generations={generations}
        activeGenerationId={summary?.active_generation?.id}
        currentContentVersion={summary?.content_version}
        canManage={canManage}
        activateGeneration={(generation, archiveManualEdits) =>
          activationMutation
            .mutateAsync({ generation, archiveManualEdits })
            .then(() => undefined)
        }
      />
    </div>
  )
}

export const Route = createFileRoute(
  '/_authenticated/workspaces/$workspaceSlug/kb/$kbId/indexes'
)({
  validateSearch: indexesSearchSchema,
  loader: ({ context, params }) =>
    Promise.all([
      context.queryClient.ensureQueryData(
        indexGenerationsQueryOptions(params.workspaceSlug, params.kbId)
      ),
      context.queryClient.ensureQueryData(
        knowledgeBaseSummaryQueryOptions(params.workspaceSlug, params.kbId)
      ),
      context.queryClient.ensureQueryData(
        selectableModelsQueryOptions(params.workspaceSlug)
      ),
      context.queryClient.ensureQueryData(meQueryOptions()),
    ]),
  staticData: {
    breadcrumb: { label: 'routes.workspaces.kb.indexes.breadcrumb' },
  },
  component: IndexesPage,
})
