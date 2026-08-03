import { describe, expect, it } from 'vitest'
import { jobsQueryOptions } from './queries'
import { jobSummaryPageSchema } from './schemas'

const kbId = 'de305d54-75b4-431b-adb2-eb6b9e546014'
const jobId = '184d3f72-7840-4b35-a943-3d5c68a9064f'

describe('jobSummaryPageSchema', () => {
  it('keeps readable job fields and drops unsafe payload fields', () => {
    const result = jobSummaryPageSchema.parse({
      items: [
        {
          id: jobId,
          status: 'running',
          action_label: '导入文件',
          target_type: 'document',
          target_display_name: 'installation.md',
          attempts: 1,
          created_at: '2026-08-01T10:00:00Z',
          updated_at: '2026-08-01T10:01:00Z',
          payload: { credential: 'must-not-survive' },
        },
      ],
      next_cursor: 'opaque-cursor',
    })

    expect(result.items[0]?.target_display_name).toBe('installation.md')
    expect(result.items[0]).not.toHaveProperty('payload')
  })

  it('uses filters in the exact jobs query key', () => {
    const filters = { status: 'running' as const, limit: 20 }
    expect(jobsQueryOptions('acme', kbId, filters).queryKey).toEqual([
      'jobs',
      'acme',
      kbId,
      filters,
    ])
  })
})
