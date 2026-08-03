import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import type { Role } from '@/features/auth/types'
import { parseApiError } from '@/lib/api/error'
import { formatDateTime } from '@/lib/i18n/datetime'
import { revokeInvitation, revokeInvitationAsPlatformAdmin } from './api'
import { invitationsQueryOptions } from './queries'
import type { InvitationListItem, InvitationStatus } from './types'

type InvitationActor = {
  actorRole: Role
  actorUserId: string
  isPlatformAdmin: boolean
}

export function canRevokeInvitation(
  invitation: InvitationListItem,
  actor: InvitationActor
) {
  if (invitation.status !== 'pending') return false
  if (actor.isPlatformAdmin || actor.actorRole === 'owner') return true
  return (
    actor.actorRole === 'admin' && invitation.created_by === actor.actorUserId
  )
}

const statusTone: Record<InvitationStatus, 'success' | 'warning' | 'neutral'> =
  {
    pending: 'warning',
    accepted: 'success',
    expired: 'neutral',
    revoked: 'neutral',
  }

type InvitationListProps = InvitationActor & { workspaceSlug: string }

export function InvitationList({
  workspaceSlug,
  actorRole,
  actorUserId,
  isPlatformAdmin,
}: InvitationListProps) {
  const { t } = useTranslation()
  const [selected, setSelected] = useState<InvitationListItem>()
  const queryClient = useQueryClient()
  const { data: invitations = [] } = useQuery(
    invitationsQueryOptions(workspaceSlug)
  )
  const statusLabel: Record<InvitationStatus, string> = {
    pending: t('invitations.list.status.pending'),
    accepted: t('invitations.list.status.accepted'),
    expired: t('invitations.list.status.expired'),
    revoked: t('invitations.list.status.revoked'),
  }
  const actor = { actorRole, actorUserId, isPlatformAdmin }
  const revokeMutation = useMutation({
    mutationFn: async (invitation: InvitationListItem) => {
      if (isPlatformAdmin) {
        await revokeInvitationAsPlatformAdmin(invitation.id)
      } else {
        await revokeInvitation(workspaceSlug, invitation.id)
      }
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ['invitations', workspaceSlug],
      })
      toast.success(t('invitations.revoke.successToast'))
      setSelected(undefined)
    },
  })
  const formatDate = (value: string) =>
    formatDateTime(value, {
      dateStyle: 'medium',
      timeStyle: 'short',
    })

  const revokeButton = (item: InvitationListItem) =>
    canRevokeInvitation(item, actor) ? (
      <Button variant='outline' size='sm' onClick={() => setSelected(item)}>
        <Trash2 />
        {t('invitations.list.revokeButton')}
      </Button>
    ) : null

  return (
    <>
      <div className='hidden overflow-hidden rounded-lg border md:block'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('invitations.list.columnEmail')}</TableHead>
              <TableHead>{t('invitations.list.columnRole')}</TableHead>
              <TableHead>{t('invitations.list.columnStatus')}</TableHead>
              <TableHead>{t('invitations.list.columnTokenPrefix')}</TableHead>
              <TableHead>{t('invitations.list.columnExpiresAt')}</TableHead>
              <TableHead className='text-right'>
                {t('invitations.list.columnActions')}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {invitations.map((item) => (
              <TableRow key={item.id}>
                <TableCell className='font-medium'>
                  {item.invited_email}
                </TableCell>
                <TableCell>{item.role}</TableCell>
                <TableCell>
                  <StatusBadge tone={statusTone[item.status]}>
                    {statusLabel[item.status]}
                  </StatusBadge>
                </TableCell>
                <TableCell className='font-mono'>{item.token_prefix}</TableCell>
                <TableCell>{formatDate(item.expires_at)}</TableCell>
                <TableCell className='text-right'>
                  {revokeButton(item)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      <div className='grid gap-3 md:hidden'>
        {invitations.map((item) => (
          <Card key={item.id}>
            <CardContent className='space-y-4'>
              <div>
                <div className='break-all font-medium'>
                  {item.invited_email}
                </div>
                <div className='mt-1 font-mono text-muted-foreground text-xs'>
                  {t('invitations.list.tokenPrefixValue', {
                    prefix: item.token_prefix,
                  })}
                </div>
              </div>
              <div className='flex items-center justify-between'>
                <StatusBadge tone={statusTone[item.status]}>
                  {statusLabel[item.status]}
                </StatusBadge>
                <span className='text-muted-foreground text-sm'>
                  {item.role} · {formatDate(item.expires_at)}
                </span>
              </div>
              <div className='flex justify-end'>{revokeButton(item)}</div>
            </CardContent>
          </Card>
        ))}
      </div>

      {invitations.length === 0 && (
        <div className='rounded-lg border border-dashed p-10 text-center text-muted-foreground text-sm'>
          {t('invitations.list.empty')}
        </div>
      )}

      <ConfirmDialog
        open={selected !== undefined}
        onOpenChange={(open) => !open && setSelected(undefined)}
        title={t('invitations.revoke.dialogTitle')}
        desc={t('invitations.revoke.dialogDescription')}
        cancelBtnText={t('invitations.revoke.cancelButton')}
        confirmText={t('invitations.revoke.confirmButton')}
        destructive
        isLoading={revokeMutation.isPending}
        handleConfirm={() => selected && revokeMutation.mutate(selected)}
      >
        {revokeMutation.isError && (
          <p className='text-destructive text-sm' role='alert'>
            {parseApiError(revokeMutation.error).message}
          </p>
        )}
      </ConfirmDialog>
    </>
  )
}
