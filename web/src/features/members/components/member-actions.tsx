import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { KeyRound, Loader2, ShieldCheck, Trash2 } from 'lucide-react'
import { useId, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { PasswordInput } from '@/components/password-input'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import type { Role } from '@/features/auth/types'
import { parseApiError } from '@/lib/api/error'
import i18n from '@/lib/i18n'
import { changeMemberRole, removeMember, resetUserPassword } from '../api'
import {
  type MemberRoleFormValues,
  memberRoleSchema,
  type PasswordResetFormValues,
  passwordResetSchema,
} from '../schemas'
import type { Member } from '../types'

export function memberActionErrorMessage(error: unknown) {
  const apiError = parseApiError(error)
  if (apiError.status === 409 && apiError.code === 'conflict') {
    return i18n.t('members.actions.lastOwnerConflict')
  }
  return apiError.message
}

type MemberActionsProps = {
  workspaceSlug: string
  actorRole: Role
  isPlatformAdmin: boolean
  member: Member
}

export function MemberActions({
  workspaceSlug,
  actorRole,
  isPlatformAdmin,
  member,
}: MemberActionsProps) {
  const { t } = useTranslation()
  const [roleOpen, setRoleOpen] = useState(false)
  const [removeOpen, setRemoveOpen] = useState(false)
  const [resetOpen, setResetOpen] = useState(false)
  const roleFormId = useId()
  const passwordResetFormId = useId()
  const queryClient = useQueryClient()
  const canManageMembership = actorRole === 'owner'
  const roleForm = useForm<MemberRoleFormValues>({
    resolver: zodResolver(memberRoleSchema),
    defaultValues: { role: member.role },
  })
  const resetForm = useForm<PasswordResetFormValues>({
    resolver: zodResolver(passwordResetSchema),
    defaultValues: { new_password: '', confirm_password: '' },
  })
  const refreshMembership = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['members', workspaceSlug] }),
      queryClient.invalidateQueries({ queryKey: ['me'] }),
    ])
  }
  const roleMutation = useMutation({
    mutationFn: (role: Role) =>
      changeMemberRole(workspaceSlug, member.user_id, role),
    onSuccess: async () => {
      await refreshMembership()
      toast.success(t('members.actions.roleUpdatedToast'))
      setRoleOpen(false)
    },
    onError: async (error) => {
      if (parseApiError(error).status === 409) {
        await queryClient.invalidateQueries({
          queryKey: ['members', workspaceSlug],
        })
      }
    },
  })
  const removeMutation = useMutation({
    mutationFn: () => removeMember(workspaceSlug, member.user_id),
    onSuccess: async () => {
      await refreshMembership()
      toast.success(t('members.actions.memberRemovedToast'))
      setRemoveOpen(false)
    },
    onError: async (error) => {
      if (parseApiError(error).status === 409) {
        await queryClient.invalidateQueries({
          queryKey: ['members', workspaceSlug],
        })
      }
    },
  })
  const resetMutation = useMutation({
    mutationFn: (password: string) =>
      resetUserPassword(member.user_id, password),
    onSuccess: () => {
      toast.success(t('members.actions.passwordResetToast'))
      setResetOpen(false)
      resetForm.reset()
    },
  })

  if (!canManageMembership && !isPlatformAdmin) return null

  return (
    <div className='flex flex-wrap justify-end gap-2'>
      {canManageMembership && (
        <>
          <Button variant='outline' size='sm' onClick={() => setRoleOpen(true)}>
            <ShieldCheck />
            {t('members.actions.adjustRoleButton')}
          </Button>
          <Button
            variant='outline'
            size='sm'
            onClick={() => setRemoveOpen(true)}
          >
            <Trash2 />
            {t('members.actions.removeMemberButton')}
          </Button>
        </>
      )}
      {isPlatformAdmin && (
        <Button variant='outline' size='sm' onClick={() => setResetOpen(true)}>
          <KeyRound />
          {t('members.actions.resetPasswordButton')}
        </Button>
      )}

      <Dialog open={roleOpen} onOpenChange={setRoleOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('members.actions.roleDialogTitle')}</DialogTitle>
            <DialogDescription>
              {t('members.actions.roleDialogDescription')}
            </DialogDescription>
          </DialogHeader>
          <Form {...roleForm}>
            <form
              id={roleFormId}
              onSubmit={roleForm.handleSubmit((values) =>
                roleMutation.mutate(values.role)
              )}
              className='space-y-4'
            >
              <FormField
                control={roleForm.control}
                name='role'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('members.actions.roleLabel')}</FormLabel>
                    <FormControl>
                      <select
                        className='h-9 w-full rounded-md border bg-background px-3 text-sm'
                        {...field}
                      >
                        <option value='member'>
                          {t('members.list.role.member')}
                        </option>
                        <option value='admin'>
                          {t('members.list.role.admin')}
                        </option>
                        <option value='owner'>
                          {t('members.list.role.owner')}
                        </option>
                      </select>
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              {roleMutation.isError && (
                <p className='text-destructive text-sm' role='alert'>
                  {memberActionErrorMessage(roleMutation.error)}
                </p>
              )}
            </form>
          </Form>
          <DialogFooter>
            <Button variant='outline' onClick={() => setRoleOpen(false)}>
              {t('members.actions.cancelButton')}
            </Button>
            <Button form={roleFormId} disabled={roleMutation.isPending}>
              {roleMutation.isPending && <Loader2 className='animate-spin' />}
              {t('members.actions.saveRoleButton')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={removeOpen}
        onOpenChange={setRemoveOpen}
        title={t('members.actions.removeDialogTitle')}
        desc={t('members.actions.removeDialogDescription')}
        cancelBtnText={t('members.actions.cancelButton')}
        confirmText={t('members.actions.confirmRemoveButton')}
        destructive
        isLoading={removeMutation.isPending}
        handleConfirm={() => removeMutation.mutate()}
      >
        {removeMutation.isError && (
          <p className='text-destructive text-sm' role='alert'>
            {memberActionErrorMessage(removeMutation.error)}
          </p>
        )}
      </ConfirmDialog>

      <Dialog open={resetOpen} onOpenChange={setResetOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('members.actions.resetDialogTitle')}</DialogTitle>
            <DialogDescription>
              {t('members.actions.resetDialogDescription')}
            </DialogDescription>
          </DialogHeader>
          <Form {...resetForm}>
            <form
              id={passwordResetFormId}
              onSubmit={resetForm.handleSubmit((values) =>
                resetMutation.mutate(values.new_password)
              )}
              className='space-y-4'
            >
              <FormField
                control={resetForm.control}
                name='new_password'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {t('members.actions.newPasswordLabel')}
                    </FormLabel>
                    <FormControl>
                      <PasswordInput autoComplete='new-password' {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={resetForm.control}
                name='confirm_password'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {t('members.actions.confirmPasswordLabel')}
                    </FormLabel>
                    <FormControl>
                      <PasswordInput autoComplete='new-password' {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              {resetMutation.isError && (
                <p className='text-destructive text-sm' role='alert'>
                  {parseApiError(resetMutation.error).message}
                </p>
              )}
            </form>
          </Form>
          <DialogFooter>
            <Button variant='outline' onClick={() => setResetOpen(false)}>
              {t('members.actions.cancelButton')}
            </Button>
            <Button
              form={passwordResetFormId}
              disabled={resetMutation.isPending}
            >
              {resetMutation.isPending && <Loader2 className='animate-spin' />}
              {t('members.actions.resetPasswordButton')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
