import { beforeEach } from 'vitest'
import i18n, { STORAGE_KEY } from '@/lib/i18n'

/**
 * 测试环境统一使用中文（zh）作为 i18n 语言：
 * 现有测试断言的是中文 UI 文本，i18n 后渲染结果必须保持中文。
 * 同时清除 localStorage 中的语言残留，避免 LanguageDetector 干扰。
 */
beforeEach(() => {
  try {
    localStorage.removeItem(STORAGE_KEY)
  } catch {
    // 某些测试环境（如隐私模式）可能禁用 localStorage
  }
  void i18n.changeLanguage('zh')
})
