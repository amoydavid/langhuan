import {
  queryOptions,
  useMutation,
  useQueryClient,
} from '@tanstack/react-query'
import { toast } from 'sonner'
import { parseApiError } from '@/lib/api/error'
import i18n from '@/lib/i18n'
import {
  createAPIKey,
  getAPIKey,
  listAPIKeys,
  revealAPIKey,
  revokeAPIKey,
  updateAPIKey,
} from './api'
import type { APIKeyCreateInput, APIKeyUpdateInput } from './schemas'

export function apiKeysQueryOptions(workspaceSlug: string) {
  return queryOptions({
    queryKey: ['api-keys', workspaceSlug],
    queryFn: () => listAPIKeys(workspaceSlug),
    staleTime: 15_000,
  })
}

export function apiKeyQueryOptions(workspaceSlug: string, apiKeyId: string) {
  return queryOptions({
    queryKey: ['api-key', workspaceSlug, apiKeyId],
    queryFn: () => getAPIKey(workspaceSlug, apiKeyId),
    staleTime: 15_000,
  })
}

export function useCreateAPIKey(workspaceSlug: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: APIKeyCreateInput) =>
      createAPIKey(workspaceSlug, input),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ['api-keys', workspaceSlug],
      })
    },
  })
}

export function useUpdateAPIKey(workspaceSlug: string, apiKeyId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: APIKeyUpdateInput) =>
      updateAPIKey(workspaceSlug, apiKeyId, input),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: ['api-keys', workspaceSlug],
        }),
        queryClient.invalidateQueries({
          queryKey: ['api-key', workspaceSlug, apiKeyId],
        }),
      ])
      toast.success(i18n.t('apiKeys.queries.updatedToast'))
    },
  })
}

// Reveal 的明文绝不写入 query cache；onSuccess 只负责失效列表，
// 明文通过返回值交给调用方的组件本地 state。
export function useRevealAPIKey(workspaceSlug: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (apiKeyId: string) => revealAPIKey(workspaceSlug, apiKeyId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ['api-keys', workspaceSlug],
      })
    },
  })
}

export function useRevokeAPIKey(workspaceSlug: string, apiKeyId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => revokeAPIKey(workspaceSlug, apiKeyId),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: ['api-keys', workspaceSlug],
        }),
        queryClient.invalidateQueries({
          queryKey: ['api-key', workspaceSlug, apiKeyId],
        }),
      ])
      toast.success(i18n.t('apiKeys.queries.revokedToast'))
    },
    onError: (error) => {
      toast.error(parseApiError(error).message)
    },
  })
}
