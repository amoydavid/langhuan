import type { QueryClient } from '@tanstack/react-query'
import type { ModelScope } from './types'

export function invalidateSelectableModels(
  queryClient: QueryClient,
  scope: ModelScope,
  workspaceSlug?: string
) {
  return queryClient.invalidateQueries({
    queryKey:
      scope === 'workspace' && workspaceSlug
        ? ['models', 'workspace', workspaceSlug, 'selectable']
        : ['models', 'workspace'],
  })
}
