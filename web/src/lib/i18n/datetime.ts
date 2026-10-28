import i18n from './index'

/**
 * 当前界面语言的 ICU locale 标识：
 * 中文 → zh-CN，其它（英文）→ en-US。
 * 日期/时间格式化统一走这里，保证切换语言后日期表达式随界面语言变化。
 */
export function currentLocale(): string {
  return i18n.language.startsWith('zh') ? 'zh-CN' : 'en-US'
}

/** 按当前界面语言格式化日期时间；options 缺省时使用 Intl 默认格式。 */
export function formatDateTime(
  value: string | number | Date,
  options?: Intl.DateTimeFormatOptions
): string {
  const date = value instanceof Date ? value : new Date(value)
  // 非法输入（如脏数据日期串）返回占位符，避免 Intl.DateTimeFormat 抛 RangeError。
  if (Number.isNaN(date.getTime())) return '—'
  return new Intl.DateTimeFormat(currentLocale(), options).format(date)
}
