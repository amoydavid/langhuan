import { describe, expect, it } from 'vitest'
import { documentQueryOptions } from './queries'
import type { Document } from './types'

describe('documentQueryOptions', () => {
  it('polls pending uploads and stops after the document becomes ready', () => {
    const options = documentQueryOptions('acme', crypto.randomUUID())
    const interval = options.refetchInterval

    expect(interval).toBeTypeOf('function')
    if (typeof interval !== 'function') return
    const queryWithStatus = (status: Document['status']) =>
      ({
        state: { data: { status } },
      }) as unknown as Parameters<typeof interval>[0]
    expect(interval(queryWithStatus('pending'))).toBe(2_000)
    expect(interval(queryWithStatus('processing'))).toBe(2_000)
    expect(interval(queryWithStatus('ready'))).toBe(false)
  })
})
