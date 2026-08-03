import { Outlet } from '@tanstack/react-router'
import { Languages, Palette } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Separator } from '@/components/ui/separator'
import { SidebarNav } from './components/sidebar-nav'

export function Settings() {
  const { t } = useTranslation()
  const sidebarNavItems = [
    {
      title: t('settings.nav.appearance'),
      href: '/settings/appearance',
      icon: <Palette size={18} />,
    },
    {
      title: t('settings.nav.language'),
      href: '/settings/language',
      icon: <Languages size={18} />,
    },
  ]
  return (
    <div className='flex min-h-[calc(100svh-7rem)] flex-col'>
      <div className='space-y-0.5'>
        <h1 className='font-bold text-2xl tracking-tight md:text-3xl'>
          {t('settings.title')}
        </h1>
        <p className='text-muted-foreground'>{t('settings.description')}</p>
      </div>
      <Separator className='my-4 lg:my-6' />
      <div className='flex flex-1 flex-col gap-2 overflow-hidden lg:flex-row lg:gap-12'>
        <aside className='top-20 lg:sticky lg:w-1/5'>
          <SidebarNav items={sidebarNavItems} />
        </aside>
        <div className='flex w-full overflow-y-hidden p-1'>
          <Outlet />
        </div>
      </div>
    </div>
  )
}
