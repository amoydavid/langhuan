import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Loader2, UserPlus } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { PasswordInput } from '@/components/password-input'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { registerUser } from '@/features/auth/api'
import {
  LAST_WORKSPACE_SLUG_KEY,
  workspaceEntry,
} from '@/features/auth/navigation'
import { meQueryOptions } from '@/features/auth/queries'
import {
  type InvitationRegistrationFormValues,
  invitationRegistrationSchema,
} from '@/features/auth/schemas'
import type { PublicInvitation } from '@/features/auth/types'
import { parseApiError } from '@/lib/api/error'
import { resetUnauthorizedNavigation } from '@/lib/query-client'
import { cn } from '@/lib/utils'

type InvitationRegistrationFormProps = React.HTMLAttributes<HTMLFormElement> & {
  invitation: PublicInvitation
  token: string
}

export function InvitationRegistrationForm({
  invitation,
  token,
  className,
  ...props
}: InvitationRegistrationFormProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const form = useForm<InvitationRegistrationFormValues>({
    resolver: zodResolver(invitationRegistrationSchema),
    defaultValues: { nickname: '', password: '', confirm_password: '' },
  })
  const mutation = useMutation({
    mutationFn: (input: Parameters<typeof registerUser>[0]) =>
      registerUser(input),
    onSuccess: async () => {
      resetUnauthorizedNavigation()
      await queryClient.fetchQuery(meQueryOptions())
      localStorage.setItem(LAST_WORKSPACE_SLUG_KEY, invitation.workspace_slug)
      toast.success(t('auth.invitationRegistration.successToast'))
      await navigate({
        to: workspaceEntry(invitation.workspace_slug),
        replace: true,
      })
    },
  })

  function submit(values: InvitationRegistrationFormValues) {
    mutation.mutate({
      email: invitation.invited_email,
      nickname: values.nickname,
      password: values.password,
      invitation_token: token,
    })
  }

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit(submit)}
        className={cn('grid gap-4', className)}
        {...props}
      >
        <FormItem>
          <FormLabel htmlFor='invited-email'>
            {t('auth.invitationRegistration.emailLabel')}
          </FormLabel>
          <FormControl>
            <Input
              id='invited-email'
              value={invitation.invited_email}
              disabled
              readOnly
            />
          </FormControl>
        </FormItem>
        <FormField
          control={form.control}
          name='nickname'
          render={({ field }) => (
            <FormItem>
              <FormLabel>
                {t('auth.invitationRegistration.nicknameLabel')}
              </FormLabel>
              <FormControl>
                <Input autoComplete='name' {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='password'
          render={({ field }) => (
            <FormItem>
              <FormLabel>
                {t('auth.invitationRegistration.passwordLabel')}
              </FormLabel>
              <FormControl>
                <PasswordInput autoComplete='new-password' {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='confirm_password'
          render={({ field }) => (
            <FormItem>
              <FormLabel>
                {t('auth.invitationRegistration.confirmPasswordLabel')}
              </FormLabel>
              <FormControl>
                <PasswordInput autoComplete='new-password' {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        {mutation.isError && (
          <p className='text-destructive text-sm' role='alert'>
            {parseApiError(mutation.error).message}
          </p>
        )}
        <Button className='mt-1' disabled={mutation.isPending}>
          {mutation.isPending ? (
            <Loader2 className='animate-spin' />
          ) : (
            <UserPlus />
          )}
          {t('auth.invitationRegistration.submitButton')}
        </Button>
      </form>
    </Form>
  )
}
