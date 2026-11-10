import { describe, expect, it } from 'vitest'
import {
  insightsSearchSchema,
  sleepingDocumentsResponseSchema,
} from './schemas'

describe('insightsSearchSchema', () => {
  it('接受 7|14|30 的数字与字符串形态（URL 深链）', () => {
    expect(insightsSearchSchema.parse({ window: 7 })).toEqual({ window: 7 })
    expect(insightsSearchSchema.parse({ window: '14' })).toEqual({
      window: 14,
    })
    expect(insightsSearchSchema.parse({ window: '30' })).toEqual({
      window: 30,
    })
  })

  it('缺省与非法值回落默认窗口（14 由读取侧兜底）', () => {
    expect(insightsSearchSchema.parse({})).toEqual({ window: undefined })
    expect(insightsSearchSchema.parse({ window: '99' })).toEqual({
      window: undefined,
    })
    expect(insightsSearchSchema.parse({ window: 'abc' })).toEqual({
      window: undefined,
    })
  })
})

describe('sleepingDocumentsResponseSchema', () => {
  it('样本不足时后端返回 documents: null，前端兜底为 undefined 而非抛错', () => {
    const parsed = sleepingDocumentsResponseSchema.parse({
      insufficient_sample: true,
      documents: null,
    })
    expect(parsed.insufficient_sample).toBe(true)
    expect(parsed.documents ?? []).toEqual([])
  })

  it('正常清单按数组解析', () => {
    const parsed = sleepingDocumentsResponseSchema.parse({
      insufficient_sample: false,
      documents: [
        {
          document_id: '019712ab-0000-7000-8000-000000000001',
          kind: 'file',
          title: '旧文档',
          indexed_at: '2026-10-01T00:00:00Z',
        },
      ],
    })
    expect(parsed.documents).toHaveLength(1)
  })
})
