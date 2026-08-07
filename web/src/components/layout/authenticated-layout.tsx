import { Outlet, useMatches } from '@tanstack/react-router'
import { AppHeader } from '@/components/layout/app-header'
import { AppSidebar } from '@/components/layout/app-sidebar'
import { SkipToMain } from '@/components/skip-to-main'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { SearchProvider } from '@/context/search-provider'
import { getCookie } from '@/lib/cookies'
import { cn } from '@/lib/utils'

type AuthenticatedLayoutProps = {
  children?: React.ReactNode
}

export function AuthenticatedLayout({ children }: AuthenticatedLayoutProps) {
  const defaultOpen = getCookie('sidebar_state') !== 'false'
  const matches = useMatches()
  const fullHeight = matches.some((m) => m.staticData.fullHeight === true)
  return (
    <SearchProvider>
      <SidebarProvider defaultOpen={defaultOpen}>
        <SkipToMain />
        <AppSidebar />
        <SidebarInset
          className={cn(
            // Set content container, so we can use container queries
            '@container/content',

            // Keep page scrolling inside the app shell. Radix overlays lock
            // body scrolling while open; an in-shell scroll host keeps the
            // sticky header visible during that lock.
            'h-svh min-h-0 overflow-auto',

            // If layout is fixed, set the height to 100svh to prevent overflow
            'has-data-[layout=fixed]:h-svh',

            // If layout is fixed and sidebar is inset,
            // set the height to 100svh - spacing (total margins) to prevent overflow
            'peer-data-[variant=inset]:has-data-[layout=fixed]:h-[calc(100svh-(var(--spacing)*4))]'
          )}
        >
          <AppHeader fixed />
          <main
            id='content'
            data-layout={fullHeight ? 'fixed' : 'auto'}
            className={cn(
              'min-w-0 flex-1 p-4 sm:px-6 sm:py-5 lg:px-7 lg:py-6',
              fullHeight && 'flex flex-col overflow-hidden'
            )}
          >
            {children ?? <Outlet />}
          </main>
        </SidebarInset>
      </SidebarProvider>
    </SearchProvider>
  )
}
