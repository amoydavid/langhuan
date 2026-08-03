import { queryOptions } from '@tanstack/react-query'
import { listKnowledgeBaseJobs } from './api'
import { hasWaitingJobs } from './polling'
import type { JobListFilters } from './types'

export function jobsQueryOptions(
  workspaceSlug: string,
  kbId: string,
  filters: JobListFilters = {}
) {
  return queryOptions({
    queryKey: ['jobs', workspaceSlug, kbId, filters],
    queryFn: () => listKnowledgeBaseJobs(workspaceSlug, kbId, filters),
    staleTime: 0,
    refetchInterval: (query) =>
      query.state.data && hasWaitingJobs(query.state.data.items)
        ? 2_000
        : false,
  })
}
