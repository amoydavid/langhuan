import { useQuery } from '@tanstack/react-query'
import { useNavigate, useParams } from '@tanstack/react-router'
import { BookOpen, ChevronsUpDown, Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Logo } from '@/assets/logo'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from '@/components/ui/sidebar'
import {
  LAST_WORKSPACE_SLUG_KEY,
  workspaceEntry,
} from '@/features/auth/navigation'
import { meQueryOptions } from '@/features/auth/queries'

export function WorkspaceSwitcher() {
  const { t } = useTranslation()
  const { data: me, isPending } = useQuery(meQueryOptions())
  const navigate = useNavigate()
  const params = useParams({ strict: false }) as { workspaceSlug?: string }
  const { isMobile, setOpenMobile } = useSidebar()
  const active =
    me?.workspaces.find(
      (workspace) => workspace.slug === params.workspaceSlug
    ) ?? me?.workspaces[0]

  function selectWorkspace(slug: string) {
    localStorage.setItem(LAST_WORKSPACE_SLUG_KEY, slug)
    setOpenMobile(false)
    void navigate({ to: workspaceEntry(slug) })
  }

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <SidebarMenuButton
              size='lg'
              disabled={isPending || !active}
              className='h-12 border border-sidebar-border bg-sidebar-surface/50 data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground'
            >
              <div className='flex aspect-square size-8 items-center justify-center'>
                <Logo
                  variant='line'
                  className='size-8 text-sidebar-logo'
                  aria-hidden='true'
                />
              </div>
              <div className='grid flex-1 text-start text-sm leading-tight'>
                <span className='truncate font-semibold'>
                  {isPending
                    ? t('common.loading')
                    : (active?.name ?? t('common.layout.noWorkspace'))}
                </span>
                <span className='truncate text-sidebar-foreground/60 text-xs'>
                  {active
                    ? t('common.layout.roleLabel', { role: active.role })
                    : t('common.layout.createWorkspacePrompt')}
                </span>
              </div>
              <ChevronsUpDown className='ms-auto' />
            </SidebarMenuButton>
          </DropdownMenuTrigger>
          <DropdownMenuContent
            className='w-(--radix-dropdown-menu-trigger-width) min-w-56 rounded-lg'
            align='start'
            side={isMobile ? 'bottom' : 'right'}
            sideOffset={4}
          >
            <DropdownMenuLabel className='text-muted-foreground text-xs'>
              Workspaces
            </DropdownMenuLabel>
            {me?.workspaces.map((workspace, index) => (
              <DropdownMenuItem
                key={workspace.workspace_id}
                onClick={() => selectWorkspace(workspace.slug)}
                className='gap-2 p-2'
              >
                <div className='flex size-6 items-center justify-center rounded-sm border'>
                  <BookOpen className='size-4 shrink-0' />
                </div>
                <span className='min-w-0 flex-1 truncate'>
                  {workspace.name}
                </span>
                <DropdownMenuShortcut>⌘{index + 1}</DropdownMenuShortcut>
              </DropdownMenuItem>
            ))}
            {me?.user.is_platform_admin && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  className='gap-2 p-2'
                  onClick={() => void navigate({ href: '/workspaces' })}
                >
                  <div className='flex size-6 items-center justify-center rounded-md border bg-background'>
                    <Plus className='size-4' />
                  </div>
                  {t('common.layout.createWorkspace')}
                </DropdownMenuItem>
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  )
}
