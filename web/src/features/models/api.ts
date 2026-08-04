import { apiClient } from '@/lib/api/client'
import type {
  ConnectionTestResult,
  CreateModelInput,
  CreateModelProviderInput,
  Model,
  ModelProvider,
  ModelScope,
  UpdateModelInput,
  UpdateModelProviderInput,
} from './types'

function scopeBasePath(scope: ModelScope, workspaceSlug?: string) {
  if (scope === 'platform') return '/admin'
  if (!workspaceSlug) throw new Error('Workspace 模型接口需要 workspaceSlug')
  return `/workspaces/${encodeURIComponent(workspaceSlug)}`
}

export function modelProviderCollectionPath(
  scope: ModelScope,
  workspaceSlug?: string
) {
  return `${scopeBasePath(scope, workspaceSlug)}/model-providers`
}

export function modelProviderResourcePath(
  scope: ModelScope,
  providerId: string,
  workspaceSlug?: string
) {
  return `${modelProviderCollectionPath(scope, workspaceSlug)}/${encodeURIComponent(providerId)}`
}

export function modelCollectionPath(
  scope: ModelScope,
  providerId: string,
  workspaceSlug?: string
) {
  return `${modelProviderResourcePath(scope, providerId, workspaceSlug)}/models`
}

export function modelResourcePath(
  scope: ModelScope,
  modelId: string,
  workspaceSlug?: string
) {
  return `${scopeBasePath(scope, workspaceSlug)}/models/${encodeURIComponent(modelId)}`
}

export async function listModelProviders(
  scope: ModelScope,
  workspaceSlug?: string
) {
  const response = await apiClient.get<ModelProvider[]>(
    modelProviderCollectionPath(scope, workspaceSlug)
  )
  return response.data
}

export async function getModelProviderOptions(
  scope: ModelScope,
  workspaceSlug?: string
) {
  const response = await apiClient.get<{ supported_providers: string[] }>(
    `${modelProviderCollectionPath(scope, workspaceSlug)}/options`
  )
  return response.data.supported_providers
}

export async function getModelProvider(
  scope: ModelScope,
  providerId: string,
  workspaceSlug?: string
) {
  const response = await apiClient.get<ModelProvider>(
    modelProviderResourcePath(scope, providerId, workspaceSlug)
  )
  return response.data
}

export async function createModelProvider(
  scope: ModelScope,
  input: CreateModelProviderInput,
  workspaceSlug?: string
) {
  const response = await apiClient.post<ModelProvider>(
    modelProviderCollectionPath(scope, workspaceSlug),
    input
  )
  return response.data
}

export async function updateModelProvider(
  scope: ModelScope,
  providerId: string,
  input: UpdateModelProviderInput,
  workspaceSlug?: string
) {
  const response = await apiClient.patch<ModelProvider>(
    modelProviderResourcePath(scope, providerId, workspaceSlug),
    input
  )
  return response.data
}

export async function deleteModelProvider(
  scope: ModelScope,
  providerId: string,
  workspaceSlug?: string
) {
  await apiClient.delete(
    modelProviderResourcePath(scope, providerId, workspaceSlug)
  )
}

export async function listModels(
  scope: ModelScope,
  providerId: string,
  workspaceSlug?: string
) {
  const response = await apiClient.get<Model[]>(
    modelCollectionPath(scope, providerId, workspaceSlug)
  )
  return response.data
}

export async function getModel(
  scope: ModelScope,
  modelId: string,
  workspaceSlug?: string
) {
  const response = await apiClient.get<Model>(
    modelResourcePath(scope, modelId, workspaceSlug)
  )
  return response.data
}

export async function createModel(
  scope: ModelScope,
  providerId: string,
  input: CreateModelInput,
  workspaceSlug?: string
) {
  const response = await apiClient.post<Model>(
    modelCollectionPath(scope, providerId, workspaceSlug),
    input
  )
  return response.data
}

export async function updateModel(
  scope: ModelScope,
  modelId: string,
  input: UpdateModelInput,
  workspaceSlug?: string
) {
  const response = await apiClient.patch<Model>(
    modelResourcePath(scope, modelId, workspaceSlug),
    input
  )
  return response.data
}

export async function deleteModel(
  scope: ModelScope,
  modelId: string,
  workspaceSlug?: string
) {
  await apiClient.delete(modelResourcePath(scope, modelId, workspaceSlug))
}

export async function testModel(
  scope: ModelScope,
  modelId: string,
  workspaceSlug?: string
) {
  const response = await apiClient.post<ConnectionTestResult>(
    `${modelResourcePath(scope, modelId, workspaceSlug)}/test`
  )
  return response.data
}
