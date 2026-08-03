import { LanguageSwitch } from '@/components/language-switch'
import { ThemeSwitch } from '@/components/theme-switch'
import { cn } from '@/lib/utils'

/**
 * 主题 + 语言切换的组合按钮。
 * 用于没有顶栏的独立页面（登录/注册、错误页等），
 * 固定显示在页面右上角，不干扰居中内容。
 */
export function AppearanceControls({ className }: { className?: string }) {
  return (
    <div
      className={cn(
        'absolute top-4 right-4 z-10 flex items-center gap-1 sm:top-6 sm:right-6',
        className
      )}
    >
      <ThemeSwitch />
      <LanguageSwitch />
    </div>
  )
}
