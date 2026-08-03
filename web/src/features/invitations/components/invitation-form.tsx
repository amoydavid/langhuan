import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Check, Copy, Loader2, Send } from 'lucide-react'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
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
import { Input } from '@/components/ui/input'
import type { Role } from '@/features/auth/types'
import { parseApiError } from '@/lib/api/error'
import { createInvitation } from '../api'
import { type InvitationFormValues, invitationSchema } from '../schemas'
import type { CreateInvitationResponse } from '../types'

export function invitableRoles(actorRole: 'owner' | 'admin'): Role[] {
  return actorRole === 'owner'
    ? ['member', 'admin', 'owner']
    : ['member', 'admin']
}

type InvitationFormProps = {
  workspaceSlug: string
  actorRole: 'owner' | 'admin'
}

export function InvitationForm({
  workspaceSlug,
  actorRole,
}: InvitationFormProps) {
  const { t } = useTranslation()
  const [created, setCreated] = useState<CreateInvitationResponse>()
  const [copied, setCopied] = useState(false)
  const queryClient = useQueryClient()
  const roles = invitableRoles(actorRole)
  const roleLabel: Record<Role, string> = {
    member: t('invitations.form.role.member'),
    admin: t('invitations.form.role.admin'),
    owner: t('invitations.form.role.owner'),
  }
  const form = useForm<InvitationFormValues>({
    resolver: zodResolver(invitationSchema),
    defaultValues: { invited_email: '', role: 'member' },
  })
  const mutation = useMutation({
    mutationFn: (values: InvitationFormValues) =>
      createInvitation(workspaceSlug, values),
    onSuccess: async (result) => {
      await queryClient.invalidateQueries({
        queryKey: ['invitations', workspaceSlug],
      })
      setCreated(result)
      form.reset({ invited_email: '', role: 'member' })
    },
  })

  async function copyInviteURL() {
    if (!created) return
    await navigator.clipboard.writeText(created.invite_url)
    setCopied(true)
    toast.success(t('invitations.form.linkCopiedToast'))
  }

  return (
    <>
      <Form {...form}>
        <form
          onSubmit={form.handleSubmit((values) => mutation.mutate(values))}
          className='grid gap-4 md:grid-cols-[minmax(0,1fr)_11rem_auto] md:items-end'
        >
          <FormField
            control={form.control}
            name='invited_email'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('invitations.form.emailLabel')}</FormLabel>
                <FormControl>
                  <Input type='email' autoComplete='email' {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='role'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('invitations.form.roleLabel')}</FormLabel>
                <FormControl>
                  <select
                    className='h-9 w-full rounded-md border bg-background px-3 text-sm'
                    {...field}
                  >
                    {roles.map((role) => (
                      <option key={role} value={role}>
                        {roleLabel[role]}
                      </option>
                    ))}
                  </select>
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <Button disabled={mutation.isPending}>
            {mutation.isPending ? (
              <Loader2 className='animate-spin' />
            ) : (
              <Send />
            )}
            {t('invitations.form.submitButton')}
          </Button>
          {mutation.isError && (
            <p className='text-destructive text-sm md:col-span-3' role='alert'>
              {parseApiError(mutation.error).message}
            </p>
          )}
        </form>
      </Form>

      <Dialog
        open={created !== undefined}
        onOpenChange={(open) => {
          if (!open) {
            setCreated(undefined)
            setCopied(false)
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {t('invitations.form.createdDialogTitle')}
            </DialogTitle>
            <DialogDescription>
              {t('invitations.form.createdDialogDescription')}
            </DialogDescription>
          </DialogHeader>
          {created && (
            <div className='space-y-3'>
              <code className='block break-all rounded-lg border bg-muted/50 p-3 text-sm'>
                {created.invite_url}
              </code>
              <p className='font-medium text-amber-700 text-sm dark:text-amber-400'>
                {t('invitations.form.linkNotVisibleAgain')}
              </p>
            </div>
          )}
          <DialogFooter>
            <Button variant='outline' onClick={() => setCreated(undefined)}>
              {t('invitations.form.closeButton')}
            </Button>
            <Button onClick={() => void copyInviteURL()}>
              {copied ? <Check /> : <Copy />}
              {copied
                ? t('invitations.form.copiedButton')
                : t('invitations.form.copyLinkButton')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
