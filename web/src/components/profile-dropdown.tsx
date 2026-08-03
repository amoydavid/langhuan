import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { LogOut, Palette } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { SignOutDialog } from '@/components/sign-out-dialog'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { meQueryOptions } from '@/features/auth/queries'
import useDialogState from '@/hooks/use-dialog-state'

export function ProfileDropdown() {
  const { t } = useTranslation()
  const { data: me } = useQuery(meQueryOptions())
  const [open, setOpen] = useDialogState()
  const user = me?.user

  return (
    <>
      <DropdownMenu modal={false}>
        <DropdownMenuTrigger asChild>
          <Button
            variant='ghost'
            className='relative h-8 w-8 rounded-full'
            disabled={!user}
            aria-label={t('common.openUserMenu')}
          >
            <Avatar className='h-8 w-8'>
              <AvatarFallback>
                {user?.nickname.slice(0, 2) ?? t('common.brandName')}
              </AvatarFallback>
            </Avatar>
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent className='w-56' align='end' forceMount>
          <DropdownMenuLabel className='font-normal'>
            <div className='flex flex-col gap-1.5'>
              <p className='truncate font-medium text-sm leading-none'>
                {user?.nickname}
              </p>
              <p className='truncate text-muted-foreground text-xs leading-none'>
                {user?.email}
              </p>
            </div>
          </DropdownMenuLabel>
          <DropdownMenuSeparator />
          <DropdownMenuGroup>
            <DropdownMenuItem asChild>
              <Link to='/settings/appearance'>
                <Palette />
                {t('common.appearanceSettings')}
              </Link>
            </DropdownMenuItem>
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
          <DropdownMenuItem variant='destructive' onClick={() => setOpen(true)}>
            <LogOut />
            {t('common.signOut')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <SignOutDialog open={!!open} onOpenChange={setOpen} />
    </>
  )
}
