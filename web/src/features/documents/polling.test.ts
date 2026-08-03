import { describe, expect, it } from 'vitest'
import { documentPollInterval, jobPollInterval } from './polling'

describe('documentPollInterval', () => {
  it('polls active states quickly and backs off after stable responses', () => {
    expect(
      documentPollInterval({
        status: 'pending',
        stableCount: 0,
        visible: true,
      })
    ).toBe(2000)
    expect(
      documentPollInterval({
        status: 'processing',
        stableCount: 3,
        visible: true,
      })
    ).toBe(5000)
  })

  it('stops for document terminal states and hidden pages', () => {
    expect(
      documentPollInterval({
        status: 'ready',
        stableCount: 0,
        visible: true,
      })
    ).toBe(false)
    expect(
      documentPollInterval({
        status: 'failed',
        stableCount: 0,
        visible: true,
      })
    ).toBe(false)
    expect(
      documentPollInterval({
        status: 'processing',
        stableCount: 0,
        visible: false,
      })
    ).toBe(false)
  })
})

describe('jobPollInterval', () => {
  it.each(['succeeded', 'failed', 'cancelled'] as const)(
    'stops for the %s terminal state',
    (status) => {
      expect(jobPollInterval({ status, stableCount: 0, visible: true })).toBe(
        false
      )
    }
  )

  it('polls queued jobs while the page is visible', () => {
    expect(
      jobPollInterval({ status: 'queued', stableCount: 0, visible: true })
    ).toBe(2000)
  })
})
