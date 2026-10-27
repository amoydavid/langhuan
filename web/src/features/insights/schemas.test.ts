import { describe, expect, it } from 'vitest'
import { insightsSearchSchema } from './schemas'

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
