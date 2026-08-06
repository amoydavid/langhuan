import { Link } from '@tanstack/react-router'
import { MailPlus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import type { Role } from '@/features/auth/types'

type MemberPageHeaderProps = {
  workspaceSlug: string
  actorRole: Role
}

export function MemberPageHeader({
  workspaceSlug,
  actorRole,
}: MemberPageHeaderProps) {
  const { t } = useTranslation()
  const canManageInvitations = actorRole === 'owner' || actorRole === 'admin'

  return (
    <div className='flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between'>
      <div>
        <p className='page-eyebrow'>{t('routes.workspaces.members.eyebrow')}</p>
        <h1 className='font-semibold text-2xl tracking-tight'>
          {t('routes.workspaces.members.title')}
        </h1>
        <p className='mt-2 text-muted-foreground'>
          {t('routes.workspaces.members.description')}
        </p>
      </div>
      {canManageInvitations && (
        <Button asChild variant='outline' className='min-h-11 sm:min-h-9'>
          <Link
            to='/workspaces/$workspaceSlug/invitations'
            params={{ workspaceSlug }}
          >
            <MailPlus aria-hidden='true' />
            {t('members.actions.manageInvitationsButton')}
          </Link>
        </Button>
      )}
    </div>
  )
}
