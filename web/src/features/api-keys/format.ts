import i18n from '@/lib/i18n'
import { formatDateTime as formatDateTimeWithLocale } from '@/lib/i18n/datetime'

// 日期/时间格式化跟随当前界面语言（zh-CN / en-US）。

export function formatDateTime(value: string): string {
  return formatDateTimeWithLocale(value, {
    dateStyle: 'medium',
    timeStyle: 'short',
  })
}

// 将后端 expires_at（可为 null）渲染为可读文本；null 表示不限期。
export function formatExpiry(value: string | null): string {
  if (!value) return i18n.t('apiKeys.format.never')
  return formatDateTimeWithLocale(value, {
    dateStyle: 'medium',
    timeStyle: 'short',
  })
}
