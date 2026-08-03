import { useSyncExternalStore } from 'react'
import type { DocumentStatus, JobStatus } from './types'

type PollInput<TStatus> = {
  status: TStatus
  stableCount: number
  visible: boolean
}

const documentTerminalStates = new Set<DocumentStatus>([
  'ready',
  'failed',
  'deleted',
])
const jobTerminalStates = new Set<JobStatus>([
  'succeeded',
  'failed',
  'cancelled',
])

function intervalFor(stableCount: number) {
  return stableCount >= 3 ? 5_000 : 2_000
}

export function documentPollInterval({
  status,
  stableCount,
  visible,
}: PollInput<DocumentStatus>) {
  if (!visible || documentTerminalStates.has(status)) return false
  return intervalFor(stableCount)
}

export function jobPollInterval({
  status,
  stableCount,
  visible,
}: PollInput<JobStatus>) {
  if (!visible || jobTerminalStates.has(status)) return false
  return intervalFor(stableCount)
}

function subscribeVisibility(callback: () => void) {
  document.addEventListener('visibilitychange', callback)
  return () => document.removeEventListener('visibilitychange', callback)
}

function visibilitySnapshot() {
  return document.visibilityState === 'visible'
}

export function useDocumentVisibility() {
  return useSyncExternalStore(
    subscribeVisibility,
    visibilitySnapshot,
    () => true
  )
}
