import { apiClient } from '@/lib/api/client'
import {
  webSourceListResponseSchema,
  webSourceMutationResponseSchema,
  webSourceSyncResponseSchema,
  webSourceViewSchema,
} from './schemas'
import type {
  SyncWebSourceResult,
  WebSourceMutationResult,
  WebSourceView,
} from './types'

export type CreateWebSourceInput = {
  name: string
  config: Record<string, unknown>
}

export type UpdateWebSourceInput = {
  name?: string
  status?: 'active' | 'disabled'
  config?: Record<string, unknown>
}

export type DeleteWebSourceInput = {
  /** 页面处置策略：keep 保留页面（默认），remove 连同页面软删。 */
  pages: 'keep' | 'remove'
}

function webSourcesPath(workspaceSlug: string, kbId: string) {
  return `/workspaces/${encodeURIComponent(workspaceSlug)}/knowledge-bases/${encodeURIComponent(kbId)}/web-sources`
}

function webSourcePath(workspaceSlug: string, kbId: string, sourceId: string) {
  return `${webSourcesPath(workspaceSlug, kbId)}/${encodeURIComponent(sourceId)}`
}

export async function listWebSources(
  workspaceSlug: string,
  kbId: string
): Promise<WebSourceView[]> {
  const response = await apiClient.get<WebSourceView[]>(
    webSourcesPath(workspaceSlug, kbId)
  )
  return webSourceListResponseSchema.parse(response.data)
}

export async function getWebSource(
  workspaceSlug: string,
  kbId: string,
  sourceId: string
): Promise<WebSourceView> {
  const response = await apiClient.get<WebSourceView>(
    webSourcePath(workspaceSlug, kbId, sourceId)
  )
  return webSourceViewSchema.parse(response.data)
}

export async function createWebSource(
  workspaceSlug: string,
  kbId: string,
  input: CreateWebSourceInput
): Promise<WebSourceMutationResult> {
  const response = await apiClient.post<WebSourceMutationResult>(
    webSourcesPath(workspaceSlug, kbId),
    input
  )
  return webSourceMutationResponseSchema.parse(response.data)
}

export async function updateWebSource(
  workspaceSlug: string,
  kbId: string,
  sourceId: string,
  input: UpdateWebSourceInput
): Promise<WebSourceMutationResult> {
  const response = await apiClient.patch<WebSourceMutationResult>(
    webSourcePath(workspaceSlug, kbId, sourceId),
    input
  )
  return webSourceMutationResponseSchema.parse(response.data)
}

export async function syncWebSource(
  workspaceSlug: string,
  kbId: string,
  sourceId: string,
  force = false
): Promise<SyncWebSourceResult> {
  const response = await apiClient.post<SyncWebSourceResult>(
    `${webSourcePath(workspaceSlug, kbId, sourceId)}/sync`,
    { force }
  )
  return webSourceSyncResponseSchema.parse(response.data)
}

export async function deleteWebSource(
  workspaceSlug: string,
  kbId: string,
  sourceId: string,
  input: DeleteWebSourceInput
): Promise<void> {
  await apiClient.delete(webSourcePath(workspaceSlug, kbId, sourceId), {
    params: { pages: input.pages },
  })
}
