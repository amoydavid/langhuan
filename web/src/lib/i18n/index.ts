import i18n from 'i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import { initReactI18next } from 'react-i18next'
import { en } from './locales/en'
import { zh } from './locales/zh'

export const STORAGE_KEY = 'langhuan-language'

export const SUPPORTED_LANGUAGES = ['zh', 'en'] as const
export type AppLanguage = (typeof SUPPORTED_LANGUAGES)[number]

export const LANGUAGE_LABELS: Record<AppLanguage, string> = {
  zh: '简体中文',
  en: 'English',
}

/**
 * 把资源对象的值放宽为 string，只保留 key 结构。
 * 用于：
 * 1. i18next CustomTypeOptions —— 让 t() 的 key 获得完整字面量类型检查；
 * 2. en 资源 satisfies 校验 —— 保证英文资源与中文资源 key 结构完全一致。
 */
export type WidenValues<T> = {
  [K in keyof T]: T[K] extends object ? WidenValues<T[K]> : string
}

// 测试环境（vitest browser）固定 zh：避免 LanguageDetector 按浏览器语言
// （playwright 默认 en-US）初始化，导致模块级 i18n.t() 求值（如 zod schema
// 消息）取到英文。生产环境仍走 localStorage + navigator 检测。
const isTest = import.meta.env.MODE === 'test'

void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: {
      zh: { translation: zh },
      en: { translation: en },
    },
    fallbackLng: 'zh',
    supportedLngs: ['zh', 'en'],
    nonExplicitSupportedLngs: true,
    load: 'languageOnly',
    lng: isTest ? 'zh' : undefined,
    detection: isTest
      ? { order: [], caches: [] }
      : {
          order: ['localStorage', 'navigator'],
          caches: ['localStorage'],
          lookupLocalStorage: STORAGE_KEY,
        },
    interpolation: {
      // React 已做 XSS 转义，不需要 i18next 额外转义
      escapeValue: false,
    },
    returnNull: false,
    react: {
      useSuspense: false,
    },
  })

/** 切换语言并持久化到 localStorage（检测器负责持久化）。 */
export function setLanguage(lng: AppLanguage) {
  void i18n.changeLanguage(lng)
  document.documentElement.lang = lng
}

// 保持 <html lang> 与语言同步（包括初始化时检测出的语言）。
i18n.on('languageChanged', (lng) => {
  document.documentElement.lang = lng
})

declare module 'i18next' {
  interface CustomTypeOptions {
    defaultNS: 'translation'
    resources: {
      translation: WidenValues<typeof zh>
    }
  }
}

export default i18n
