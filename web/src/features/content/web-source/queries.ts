import type { QueryClient } from '@tanstack/react-query'
import { mutationOptions, queryOptions } from '@tanstack/react-query'
import {
  type CreateWebSourceInput,
  createWebSource,
  type DeleteWebSourceInput,
  deleteWebSource,
  getWebSource,
  listWebSources,
  syncWebSource,
  type UpdateWebSourceInput,
  updateWebSource,
} from './api'

export const webSourcesQueryKey = (workspaceSlug: string, kbId: string) => [
  'web-sources',
  workspaceSlug,
  kbId,
]

export function webSourcesQueryOptions(workspaceSlug: string, kbId: string) {
  return queryOptions({
    queryKey: webSourcesQueryKey(workspaceSlug, kbId),
    queryFn: () => listWebSources(workspaceSlug, kbId),
    staleTime: 5_000,
  })
}

export function webSourceQueryOptions(
  workspaceSlug: string,
  kbId: string,
  sourceId: string
) {
  return queryOptions({
    queryKey: [...webSourcesQueryKey(workspaceSlug, kbId), sourceId],
    queryFn: () => getWebSource(workspaceSlug, kbId, sourceId),
    staleTime: 5_000,
  })
}

function invalidateWebSources(
  queryClient: QueryClient,
  workspaceSlug: string,
  kbId: string
) {
  void queryClient.invalidateQueries({
    queryKey: webSourcesQueryKey(workspaceSlug, kbId),
  })
  // 源变更会联动文档（同步产出/删除页面），一并失效。
  void queryClient.invalidateQueries({
    queryKey: ['documents', workspaceSlug, kbId],
  })
}

export function createWebSourceMutationOptions(
  workspaceSlug: string,
  kbId: string,
  queryClient: QueryClient
) {
  return mutationOptions({
    mutationKey: ['web-source-create', workspaceSlug, kbId],
    mutationFn: (input: CreateWebSourceInput) =>
      createWebSource(workspaceSlug, kbId, input),
    onSuccess: () => invalidateWebSources(queryClient, workspaceSlug, kbId),
  })
}

export function updateWebSourceMutationOptions(
  workspaceSlug: string,
  kbId: string,
  sourceId: string,
  queryClient: QueryClient
) {
  return mutationOptions({
    mutationKey: ['web-source-update', workspaceSlug, kbId, sourceId],
    mutationFn: (input: UpdateWebSourceInput) =>
      updateWebSource(workspaceSlug, kbId, sourceId, input),
    onSuccess: () => invalidateWebSources(queryClient, workspaceSlug, kbId),
  })
}

export function syncWebSourceMutationOptions(
  workspaceSlug: string,
  kbId: string,
  sourceId: string,
  queryClient: QueryClient
) {
  return mutationOptions({
    mutationKey: ['web-source-sync', workspaceSlug, kbId, sourceId],
    mutationFn: (force: boolean) =>
      syncWebSource(workspaceSlug, kbId, sourceId, force),
    onSuccess: () => invalidateWebSources(queryClient, workspaceSlug, kbId),
  })
}

export function deleteWebSourceMutationOptions(
  workspaceSlug: string,
  kbId: string,
  queryClient: QueryClient
) {
  return mutationOptions({
    mutationKey: ['web-source-delete', workspaceSlug, kbId],
    mutationFn: (input: DeleteWebSourceInput & { sourceId: string }) =>
      deleteWebSource(workspaceSlug, kbId, input.sourceId, {
        pages: input.pages,
      }),
    onSuccess: () => invalidateWebSources(queryClient, workspaceSlug, kbId),
  })
}
