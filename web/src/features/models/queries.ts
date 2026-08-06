import { queryOptions } from '@tanstack/react-query'
import {
  getModelProvider,
  listModelCatalog,
  listModelProviders,
  listModels,
  listProviderModelCatalog,
  type ModelCatalogFilters,
} from './api'
import type { ModelScope, ModelType } from './types'

export function modelProvidersQueryOptions(
  scope: ModelScope,
  workspaceSlug?: string
) {
  return queryOptions({
    queryKey: ['model-providers', scope, workspaceSlug ?? null],
    queryFn: () => listModelProviders(scope, workspaceSlug),
    staleTime: 15_000,
  })
}

export function modelCatalogQueryOptions(
  scope: ModelScope,
  workspaceSlug: string | undefined,
  filters: Partial<ModelCatalogFilters>
) {
  return queryOptions({
    queryKey: ['model-catalog', scope, workspaceSlug ?? null, filters],
    queryFn: () => listModelCatalog(scope, workspaceSlug, filters),
    staleTime: 15_000,
  })
}

export function modelProviderQueryOptions(
  scope: ModelScope,
  providerId: string,
  workspaceSlug?: string
) {
  return queryOptions({
    queryKey: ['model-provider', scope, workspaceSlug ?? null, providerId],
    queryFn: () => getModelProvider(scope, providerId, workspaceSlug),
    staleTime: 15_000,
  })
}

export function providerModelCatalogQueryOptions(
  scope: ModelScope,
  providerId: string,
  type: 'embedding' | 'rerank',
  workspaceSlug?: string,
  query = ''
) {
  return queryOptions({
    queryKey: [
      'provider-model-catalog',
      scope,
      workspaceSlug ?? null,
      providerId,
      type,
      query,
    ],
    queryFn: () =>
      listProviderModelCatalog(scope, providerId, type, workspaceSlug, query),
    staleTime: 60_000,
  })
}

export function modelsQueryOptions(
  scope: ModelScope,
  providerId: string,
  workspaceSlug?: string
) {
  return queryOptions({
    queryKey: ['models', scope, workspaceSlug ?? null, providerId],
    queryFn: () => listModels(scope, providerId, workspaceSlug),
    staleTime: 15_000,
  })
}

export function selectableModelsQueryOptions(
  workspaceSlug: string,
  type: ModelType = 'embedding'
) {
  return queryOptions({
    queryKey:
      type === 'embedding'
        ? ['models', 'workspace', workspaceSlug, 'selectable']
        : ['models', 'workspace', workspaceSlug, 'selectable', type],
    queryFn: async () => {
      const providers = await listModelProviders('workspace', workspaceSlug)
      const activeProviders = providers.filter(
        (provider) => provider.status === 'active'
      )
      const modelGroups = await Promise.all(
        activeProviders.map((provider) =>
          listModels('workspace', provider.id, workspaceSlug)
        )
      )
      return modelGroups
        .flat()
        .filter(
          (model) =>
            model.type === type &&
            model.status === 'active' &&
            model.provider.status === 'active' &&
            model.available
        )
    },
    staleTime: 15_000,
  })
}
