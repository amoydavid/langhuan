import { beforeEach, describe, expect, it } from 'vitest'
import { currentLocale, formatDateTime } from './datetime'
import i18n from './index'

describe('日期/时间 i18n', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('zh')
  })

  it('中文界面使用 zh-CN locale 并输出中文日期', () => {
    expect(currentLocale()).toBe('zh-CN')
    const text = formatDateTime(new Date(2026, 0, 2, 14, 30), {
      dateStyle: 'medium',
    })
    expect(text).toContain('2026')
    expect(text).toContain('月')
  })

  it('切换英文后使用 en-US locale 并输出英文日期', async () => {
    await i18n.changeLanguage('en')
    expect(currentLocale()).toBe('en-US')
    const text = formatDateTime(new Date(2026, 0, 2, 14, 30), {
      dateStyle: 'medium',
    })
    expect(text).toMatch(/Jan/i)
    expect(text).not.toContain('月')
  })

  it('兼容 ISO 字符串与时间戳输入', () => {
    expect(
      formatDateTime('2026-08-02T10:00:00Z', { dateStyle: 'short' })
    ).toMatch(/\d{4}/)
    expect(formatDateTime(1_782_000_000_000, { dateStyle: 'short' })).toMatch(
      /\d{4}/
    )
  })
})
