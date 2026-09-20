import { mutationOptions, queryOptions } from '@tanstack/react-query'
import {
  createSourceConnection,
  deleteSourceConnection,
  getSourceConnection,
  listSourceConnections,
  testSourceConnection,
  updateSourceConnection,
} from './api'
import type {
  CreateSourceConnectionInput,
  UpdateSourceConnectionInput,
} from './types'

export function sourceConnectionsQueryOptions(workspaceSlug: string) {
  return queryOptions({
    queryKey: ['source-connections', workspaceSlug],
    queryFn: () => listSourceConnections(workspaceSlug),
    staleTime: 15_000,
  })
}

export function sourceConnectionQueryOptions(
  workspaceSlug: string,
  connectionId: string
) {
  return queryOptions({
    queryKey: ['source-connection', workspaceSlug, connectionId],
    queryFn: () => getSourceConnection(workspaceSlug, connectionId),
    staleTime: 15_000,
  })
}

export function createSourceConnectionMutationOptions(workspaceSlug: string) {
  return mutationOptions({
    mutationKey: ['source-connection-create', workspaceSlug],
    mutationFn: (input: CreateSourceConnectionInput) =>
      createSourceConnection(workspaceSlug, input),
    meta: { invalidate: [['source-connections', workspaceSlug]] },
  })
}

export function updateSourceConnectionMutationOptions(
  workspaceSlug: string,
  connectionId: string
) {
  return mutationOptions({
    mutationKey: ['source-connection-update', workspaceSlug, connectionId],
    mutationFn: (input: UpdateSourceConnectionInput) =>
      updateSourceConnection(workspaceSlug, connectionId, input),
    meta: { invalidate: [['source-connections', workspaceSlug]] },
  })
}

export function deleteSourceConnectionMutationOptions(workspaceSlug: string) {
  return mutationOptions({
    mutationKey: ['source-connection-delete', workspaceSlug],
    mutationFn: (connectionId: string) =>
      deleteSourceConnection(workspaceSlug, connectionId),
    meta: { invalidate: [['source-connections', workspaceSlug]] },
  })
}

// 连接测试结果非持久化（后端不落库，前端也是组件内临时 state），无需 invalidate。
export function testSourceConnectionMutationOptions(
  workspaceSlug: string,
  connectionId: string
) {
  return mutationOptions({
    mutationKey: ['source-connection-test', workspaceSlug, connectionId],
    mutationFn: () => testSourceConnection(workspaceSlug, connectionId),
  })
}
