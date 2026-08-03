import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  deleteModel,
  deleteModelProvider,
  testModel,
  updateModel,
  updateModelProvider,
} from './api'
import { invalidateSelectableModels } from './cache'
import { ModelProviderDetailContent } from './components/model-provider-detail-content'
import { modelProviderQueryOptions, modelsQueryOptions } from './queries'
import type {
  ConnectionTestResult,
  Model,
  ModelProvider,
  ModelScope,
} from './types'

export { ModelProviderDetailContent } from './components/model-provider-detail-content'

type ModelProviderDetailPageProps = {
  scope: ModelScope
  workspaceSlug?: string
  providerId: string
  canManage: boolean
  onProviderDeleted?: () => void
}

export function ModelProviderDetailPage({
  scope,
  workspaceSlug,
  providerId,
  canManage,
  onProviderDeleted,
}: ModelProviderDetailPageProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const providerQuery = useQuery(
    modelProviderQueryOptions(scope, providerId, workspaceSlug)
  )
  const modelsQuery = useQuery(
    modelsQueryOptions(scope, providerId, workspaceSlug)
  )
  const [testResult, setTestResult] = useState<{
    modelId: string
    result: ConnectionTestResult
  }>()

  const refresh = async () => {
    await Promise.all([
      queryClient.invalidateQueries({
        queryKey: ['model-provider', scope, workspaceSlug ?? null, providerId],
      }),
      queryClient.invalidateQueries({
        queryKey: ['models', scope, workspaceSlug ?? null, providerId],
      }),
      queryClient.invalidateQueries({
        queryKey: ['model-providers', scope, workspaceSlug ?? null],
      }),
      invalidateSelectableModels(queryClient, scope, workspaceSlug),
    ])
  }
  const providerStatusMutation = useMutation({
    mutationFn: (provider: ModelProvider) =>
      updateModelProvider(
        scope,
        provider.id,
        { status: provider.status === 'active' ? 'disabled' : 'active' },
        workspaceSlug
      ),
    onSuccess: async () => {
      await refresh()
      toast.success(t('models.detailPage.providerStatusToast'))
    },
  })
  const providerDeleteMutation = useMutation({
    mutationFn: (provider: ModelProvider) =>
      deleteModelProvider(scope, provider.id, workspaceSlug),
    onSuccess: async () => {
      await refresh()
      toast.success(t('models.detailPage.providerDeletedToast'))
      onProviderDeleted?.()
    },
  })
  const modelStatusMutation = useMutation({
    mutationFn: (model: Model) =>
      updateModel(
        scope,
        model.id,
        { status: model.status === 'active' ? 'disabled' : 'active' },
        workspaceSlug
      ),
    onSuccess: async () => {
      await refresh()
      toast.success(t('models.detailPage.modelStatusToast'))
    },
  })
  const modelDeleteMutation = useMutation({
    mutationFn: (model: Model) => deleteModel(scope, model.id, workspaceSlug),
    onSuccess: async () => {
      await refresh()
      toast.success(t('models.detailPage.modelDeletedToast'))
    },
  })
  const testMutation = useMutation({
    mutationFn: async (model: Model) => ({
      modelId: model.id,
      result: await testModel(scope, model.id, workspaceSlug),
    }),
    onSuccess: (result) => setTestResult(result),
  })

  if (!providerQuery.data || modelsQuery.data === undefined) {
    return (
      <div className='rounded-xl border border-dashed p-12 text-center text-muted-foreground text-sm'>
        {t('models.detailPage.loading')}
      </div>
    )
  }

  const busy =
    providerStatusMutation.isPending ||
    providerDeleteMutation.isPending ||
    modelStatusMutation.isPending ||
    modelDeleteMutation.isPending ||
    testMutation.isPending
  const error =
    providerStatusMutation.error ??
    providerDeleteMutation.error ??
    modelStatusMutation.error ??
    modelDeleteMutation.error ??
    testMutation.error

  return (
    <ModelProviderDetailContent
      provider={providerQuery.data}
      models={modelsQuery.data}
      routeScope={scope}
      workspaceSlug={workspaceSlug}
      canManage={canManage}
      busy={busy}
      error={error}
      testResult={testResult}
      onProviderStatusToggle={() =>
        providerStatusMutation.mutate(providerQuery.data)
      }
      onProviderDelete={() => providerDeleteMutation.mutate(providerQuery.data)}
      onModelStatusToggle={(model) => modelStatusMutation.mutate(model)}
      onModelDelete={(model) => modelDeleteMutation.mutate(model)}
      onModelTest={(model) => testMutation.mutate(model)}
    />
  )
}
