import { queryOptions } from '@tanstack/react-query'
import { getWorkspaceSearchSettings } from './api'

export function workspaceSearchSettingsQueryOptions(workspaceSlug: string) {
  return queryOptions({
    queryKey: ['workspace-search-settings', workspaceSlug],
    queryFn: () => getWorkspaceSearchSettings(workspaceSlug),
    staleTime: 15_000,
  })
}
