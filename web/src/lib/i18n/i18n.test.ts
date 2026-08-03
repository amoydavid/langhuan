import { describe, expect, it } from 'vitest'
import i18n from './index'

describe('i18n 基础设施', () => {
  it('默认使用中文（测试环境固定 zh）', () => {
    expect(i18n.language.startsWith('zh')).toBe(true)
    expect(i18n.t('settings.title')).toBe('设置')
    expect(i18n.t('errors.not_found')).toBe('资源不存在')
  })

  it('切换到英文后渲染英文文案', async () => {
    await i18n.changeLanguage('en')
    expect(i18n.language).toBe('en')
    expect(i18n.t('settings.title')).toBe('Settings')
    expect(i18n.t('errors.not_found')).toBe('Resource not found.')
    // 未知 key 回退：返回 key 本身，不抛错
    expect(i18n.t('nonexistent.key' as never)).toBe('nonexistent.key')
  })

  it('切回中文恢复中文文案', async () => {
    await i18n.changeLanguage('zh')
    expect(i18n.t('common.brandName')).toBe('琅嬛')
    expect(i18n.t('auth.signIn.submitButton')).toBeDefined()
  })
})
