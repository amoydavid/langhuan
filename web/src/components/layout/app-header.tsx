import { useTranslation } from 'react-i18next'
import { LanguageSwitch } from '@/components/language-switch'
import { ProfileDropdown } from '@/components/profile-dropdown'
import { Search } from '@/components/search'
import { ThemeSwitch } from '@/components/theme-switch'
import { Separator } from '@/components/ui/separator'
import { SidebarTrigger } from '@/components/ui/sidebar'
import { cn } from '@/lib/utils'
import { AppBreadcrumbs } from './app-breadcrumbs'

type AppHeaderProps = React.HTMLAttributes<HTMLElement> & {
  fixed?: boolean
}

export function AppHeader({
  className,
  fixed = true,
  ...props
}: AppHeaderProps) {
  const { t } = useTranslation()
  return (
    <header
      data-testid='app-header'
      className={cn(
        'z-40 h-14 shrink-0 border-b bg-background/90 backdrop-blur supports-backdrop-filter:bg-background/75',
        fixed && 'header-fixed sticky top-0 w-full',
        className
      )}
      {...props}
    >
      <div className='flex h-full min-w-0 items-center gap-2 px-4 sm:gap-3 sm:px-6 lg:px-7'>
        <span data-header-item='trigger' className='flex shrink-0 items-center'>
          <SidebarTrigger variant='ghost' className='max-md:scale-110' />
        </span>
        <Separator orientation='vertical' className='h-6' />
        <span data-header-item='breadcrumbs' className='min-w-0'>
          <AppBreadcrumbs />
        </span>
        <span data-header-item='spacer' className='flex-1' />
        <span data-header-item='search' className='hidden sm:block'>
          <Search placeholder={t('common.layout.searchPlaceholder')} />
        </span>
        <span data-header-item='theme' className='flex shrink-0'>
          <ThemeSwitch />
        </span>
        <span data-header-item='language' className='flex shrink-0'>
          <LanguageSwitch />
        </span>
        <span data-header-item='profile' className='flex shrink-0'>
          <ProfileDropdown />
        </span>
      </div>
    </header>
  )
}
