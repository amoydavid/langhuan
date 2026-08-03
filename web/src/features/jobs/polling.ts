import type { JobStatus, JobSummary } from './types'

const waitingStatuses = new Set<JobStatus>(['pending', 'queued', 'running'])
const terminalStatuses = new Set<JobStatus>([
  'completed',
  'succeeded',
  'failed',
  'cancelled',
])

export function isJobWaiting(status: JobStatus) {
  return waitingStatuses.has(status)
}

export function isJobTerminal(status: JobStatus) {
  return terminalStatuses.has(status)
}

export function hasWaitingJobs(items: JobSummary[]) {
  return items.some((item) => isJobWaiting(item.status))
}
