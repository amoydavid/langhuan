import { describe, expect, it } from 'vitest'
import { isJobTerminal, isJobWaiting } from './polling'
import type { JobStatus } from './types'

describe('job polling states', () => {
  it.each<JobStatus>(['pending', 'queued', 'running'])(
    '%s is waiting',
    (status) => {
      expect(isJobWaiting(status)).toBe(true)
      expect(isJobTerminal(status)).toBe(false)
    }
  )

  it.each<JobStatus>(['completed', 'succeeded', 'failed', 'cancelled'])(
    '%s is terminal',
    (status) => {
      expect(isJobWaiting(status)).toBe(false)
      expect(isJobTerminal(status)).toBe(true)
    }
  )
})
